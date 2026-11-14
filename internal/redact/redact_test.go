package redact_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/redact"
	"github.com/bdnix/kubelook/internal/testutil"
)

func obj(apiVersion, kind, name string, fields map[string]any) *unstructured.Unstructured {
	o := map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": map[string]any{
		"name": name, "namespace": "ns", "managedFields": []any{map[string]any{"manager": "kubectl"}},
	}}
	for k, v := range fields {
		o[k] = v
	}
	return &unstructured.Unstructured{Object: o}
}

func secret(name string, data map[string]any) *unstructured.Unstructured {
	return obj("v1", "Secret", name, map[string]any{"data": data, "type": "Opaque"})
}

func TestApply_ManagedFieldsRemovedFromAnyObject(t *testing.T) {
	o := obj("apps/v1", "Deployment", "web", map[string]any{"spec": map[string]any{"replicas": int64(2)}})
	require.True(t, redact.Apply(o))
	_, found, _ := unstructured.NestedFieldNoCopy(o.Object, "metadata", "managedFields")
	assert.False(t, found)
	assert.Equal(t, "web", o.GetName())
	replicas, _, _ := unstructured.NestedInt64(o.Object, "spec", "replicas")
	assert.EqualValues(t, 2, replicas)
}

func TestApply_Secret_MasksValuesKeepsKeysAndMetadata(t *testing.T) {
	o := secret("db", map[string]any{"password": "c2VjcmV0", "user": "YWRh"})
	o.SetLabels(map[string]string{"app": "db"})
	o.SetAnnotations(map[string]string{"keep": "me", "kubectl.kubernetes.io/last-applied-configuration": `{"data":{"password":"c2VjcmV0"}}`})
	o.Object["stringData"] = map[string]any{"plain": "visible"}

	require.True(t, redact.Apply(o))
	data, _, _ := unstructured.NestedMap(o.Object, "data")
	assert.Equal(t, map[string]any{"password": redact.Mask, "user": redact.Mask}, data)
	assert.NotContains(t, o.Object, "stringData")
	assert.Equal(t, map[string]string{"keep": "me"}, o.GetAnnotations())
	assert.Equal(t, map[string]string{"app": "db"}, o.GetLabels())
	assert.Equal(t, "Opaque", o.Object["type"])
}

func TestApply_Secret_LastAppliedOnlyAnnotationRemovesTheMap(t *testing.T) {
	o := secret("db", map[string]any{"a": "Yg=="})
	o.SetAnnotations(map[string]string{"kubectl.kubernetes.io/last-applied-configuration": "x"})
	redact.Apply(o)
	_, found, _ := unstructured.NestedFieldNoCopy(o.Object, "metadata", "annotations")
	assert.False(t, found)
}

func TestApply_Secret_WithoutDataOrWithBrokenData(t *testing.T) {
	o := obj("v1", "Secret", "empty", nil)
	require.True(t, redact.Apply(o))
	assert.NotContains(t, o.Object, "data")
	broken := obj("v1", "Secret", "odd", map[string]any{"data": "not-a-map"})
	require.True(t, redact.Apply(broken))
	assert.NotContains(t, broken.Object, "data")
}

func TestApply_HelmReleases_AreDropped(t *testing.T) {
	byType := obj("v1", "Secret", "anything", map[string]any{"type": "helm.sh/release.v1", "data": map[string]any{"release": "x"}})
	byName := secret("sh.helm.release.v1.web.v3", map[string]any{"release": "x"})
	cm := obj("v1", "ConfigMap", "sh.helm.release.v1.web.v1", map[string]any{"data": map[string]any{"release": "x"}})
	cm.SetLabels(map[string]string{"owner": "helm"})
	assert.False(t, redact.Apply(byType))
	assert.False(t, redact.Apply(byName))
	assert.False(t, redact.Apply(cm))
}

func TestApply_OtherConfigMapsAndNonSecretAnnotationsUnchanged(t *testing.T) {
	plain := obj("v1", "ConfigMap", "settings", map[string]any{"data": map[string]any{"k": "v"}})
	notHelm := obj("v1", "ConfigMap", "sh.helm.release.v1.fake", map[string]any{"data": map[string]any{"k": "v"}}) // no owner label
	deploy := obj("apps/v1", "Deployment", "web", nil)
	deploy.SetAnnotations(map[string]string{"kubectl.kubernetes.io/last-applied-configuration": "{}"})
	assert.True(t, redact.Apply(plain))
	assert.True(t, redact.Apply(notHelm))
	assert.True(t, redact.Apply(deploy))
	data, _, _ := unstructured.NestedMap(plain.Object, "data")
	assert.Equal(t, map[string]any{"k": "v"}, data)
	assert.Contains(t, deploy.GetAnnotations(), "kubectl.kubernetes.io/last-applied-configuration")
}

func TestList_RedactsEachAndDropsHidden(t *testing.T) {
	items := []unstructured.Unstructured{
		*secret("a", map[string]any{"k": "dg=="}),
		*secret("sh.helm.release.v1.x.v1", map[string]any{"release": "x"}),
		*secret("b", map[string]any{"k": "dg=="}),
	}
	got := redact.List(items)
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].GetName())
	assert.Equal(t, "b", got[1].GetName())
	data, _, _ := unstructured.NestedMap(got[1].Object, "data")
	assert.Equal(t, redact.Mask, data["k"])
}

func TestManifest_MasksSecretsKeepsOtherDocumentsByteForByte(t *testing.T) {
	cm := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name:   spaced   # keep this comment\ndata:\n  a: \"1\"\n"
	sec := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db\ndata:\n  password: c2VjcmV0\nstringData:\n  plain: visible\n"
	text := "---\n# Source: chart/templates/cm.yaml\n" + cm + "---\n" + sec + "---\n" + "not: [valid\n" + "---\n# only a comment\n"
	got := redact.Manifest(text)

	assert.Contains(t, got, cm, "ConfigMaps are untouched")
	assert.NotContains(t, got, "c2VjcmV0")
	assert.NotContains(t, got, "visible")
	assert.Contains(t, got, "password: '"+redact.Mask+"'")
	assert.Contains(t, got, "# unparseable document removed")
	assert.NotContains(t, got, "not: [valid")
	assert.Contains(t, got, "# only a comment")
	assert.Equal(t, strings.Count(text, "---\n"), strings.Count(got, "---\n"), "separators are kept")
}

func TestManifest_HelmReleaseSecretAndEmptyInput(t *testing.T) {
	rel := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: sh.helm.release.v1.x.v1\ndata:\n  release: eHl6\n"
	assert.NotContains(t, redact.Manifest(rel), "eHl6")
	assert.Equal(t, "", redact.Manifest(""))
	assert.Equal(t, "# nothing\n", redact.Manifest("# nothing\n"))
}

func TestUnmask_ReplacesMaskedValuesWithLiveOnes(t *testing.T) {
	live := secret("db", map[string]any{"password": "bGl2ZQ==", "user": "YWRh"})
	edited := secret("db", map[string]any{"password": redact.Mask, "user": "bmV3dXNlcg==", "extra": "ZXh0cmE="})
	got, err := redact.Unmask(edited, live)
	require.NoError(t, err)
	data, _, _ := unstructured.NestedMap(got.Object, "data")
	assert.Equal(t, map[string]any{"password": "bGl2ZQ==", "user": "bmV3dXNlcg==", "extra": "ZXh0cmE="}, data)
	before, _, _ := unstructured.NestedMap(edited.Object, "data")
	assert.Equal(t, redact.Mask, before["password"], "the edited object is not modified")
}

func TestUnmask_MaskedKeyGoneLive_Conflict(t *testing.T) {
	live := secret("db", map[string]any{"user": "YWRh"})
	edited := secret("db", map[string]any{"password": redact.Mask})
	_, err := redact.Unmask(edited, live)
	require.ErrorIs(t, err, redact.ErrConflict)
	require.ErrorIs(t, err, apperr.ErrConflict)
}

func TestUnmask_NonSecretsAndSecretsWithoutData(t *testing.T) {
	cm := obj("v1", "ConfigMap", "c", map[string]any{"data": map[string]any{"k": redact.Mask}})
	got, err := redact.Unmask(cm, cm)
	require.NoError(t, err)
	assert.Same(t, cm, got)

	noData := obj("v1", "Secret", "s", nil)
	got, err = redact.Unmask(noData, noData)
	require.NoError(t, err)
	assert.Equal(t, noData.Object, got.Object)
}

func TestApply_Property_NoOriginalValueSurvives(t *testing.T) {
	r := testutil.Rand("redact-property")
	for i := range 200 {
		data, plain := map[string]any{}, map[string]any{}
		var secrets []string
		for k := range 1 + i%5 {
			buf := make([]byte, 24)
			_, err := io.ReadFull(r, buf)
			require.NoError(t, err)
			v := base64.StdEncoding.EncodeToString(buf)
			data[fmt.Sprintf("key%d", k)] = v
			secrets = append(secrets, v)
		}
		pbuf := make([]byte, 12)
		_, err := io.ReadFull(r, pbuf)
		require.NoError(t, err)
		pv := base64.RawStdEncoding.EncodeToString(pbuf)
		plain["p"] = pv
		secrets = append(secrets, pv)

		o := secret(fmt.Sprintf("s%d", i), data)
		o.Object["stringData"] = plain
		o.SetAnnotations(map[string]string{"kubectl.kubernetes.io/last-applied-configuration": fmt.Sprintf(`{"data":{"k":%q}}`, secrets[0])})
		require.True(t, redact.Apply(o))
		raw, err := json.Marshal(o.Object)
		require.NoError(t, err)
		for _, v := range secrets {
			assert.NotContains(t, string(raw), v)
		}
	}
}
