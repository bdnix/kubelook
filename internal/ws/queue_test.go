package ws_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/ws"
)

type fakeChannel struct{ closed int }

func (f *fakeChannel) Handle(context.Context, string, json.RawMessage) error { return nil }
func (f *fakeChannel) Close()                                                { f.closed++ }

func types(c *ws.Conn) []string {
	var out []string
	for _, m := range c.Pending() {
		out = append(out, fmt.Sprintf("%v:%v", m["ch"], m["t"]))
	}
	return out
}

func TestConn_Send_FullQueueOfMustDeliverFrames_SlowConsumerClosesTheChannel(t *testing.T) {
	c := ws.DetachedConn(3)
	chn := &fakeChannel{}
	c.AddChannel(1, chn)
	require.NoError(t, c.Send(1, "a", nil))
	require.NoError(t, c.Send(1, "b", nil))
	require.NoError(t, c.Send(1, "c", nil))
	err := c.Send(1, "d", nil)
	require.ErrorIs(t, err, ws.ErrSlowConsumer)
	assert.Equal(t, 1, chn.closed)
	assert.Equal(t, []string{"1:a", "1:b", "1:c", "1:closed"}, types(c), "the closed frame goes through despite the full queue")
	last := c.Pending()[3]
	assert.Equal(t, "slow_consumer", last["d"].(map[string]any)["reason"])

	// the channel is gone: closing it again does nothing
	c.CloseChannel(1, "again")
	assert.Equal(t, 1, chn.closed)
	assert.Len(t, c.Pending(), 4)
}

func TestConn_SendDroppable_FullQueueEvictsTheOldestDroppableOfTheSameChannel(t *testing.T) {
	c := ws.DetachedConn(3)
	c.SendDroppable(1, "x1", nil)
	require.NoError(t, c.Send(1, "keep", nil))
	c.SendDroppable(2, "y1", nil)
	c.SendDroppable(1, "x2", nil) // evicts x1, not y1
	assert.Equal(t, []string{"1:keep", "2:y1", "1:x2"}, types(c))
	assert.Equal(t, 1, c.Dropped(1))
	assert.Equal(t, 0, c.Dropped(2))
}

func TestConn_SendDroppable_NothingToEvict_TheNewFrameIsDropped(t *testing.T) {
	c := ws.DetachedConn(2)
	require.NoError(t, c.Send(1, "a", nil))
	c.SendDroppable(2, "z1", nil)
	c.SendDroppable(1, "x", nil) // no droppable frame of channel 1 → dropped itself
	assert.Equal(t, []string{"1:a", "2:z1"}, types(c))
	assert.Equal(t, 1, c.Dropped(1))
}

func TestConn_Send_FullQueue_EvictsADroppableFrameOfAnyChannel(t *testing.T) {
	c := ws.DetachedConn(2)
	c.SendDroppable(2, "z1", nil)
	require.NoError(t, c.Send(1, "a", nil))
	require.NoError(t, c.Send(1, "b", nil), "z1 makes room")
	assert.Equal(t, []string{"1:a", "1:b"}, types(c))
	assert.Equal(t, 1, c.Dropped(2))
}
