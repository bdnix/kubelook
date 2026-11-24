import { expect, test } from "@playwright/test";
import {
  clusterRail,
  editorText,
  guardPage,
  pageLog,
  kubectl,
  openAlpha,
  podOf,
  signIn,
  signInWithToken,
} from "../helpers";

test.describe("the real stack in a browser", () => {
  let problems: string[] = [];
  test.beforeEach(({ page }) => {
    problems = guardPage(page);
  });
  // (9) no page of any test has a Content Security Policy violation or an uncaught error
  test.afterEach(async ({ page }, testInfo) => {
    if (testInfo.status !== testInfo.expectedStatus) {
      // what the page said and drew, for the CI log (the trace is an artifact only)
      console.log(`--- page log of "${testInfo.title}"\n${pageLog(page)}`);
      const drawer = await page
        .evaluate(
          () =>
            document
              .querySelector("aside[aria-label=Details]")
              ?.innerText.slice(0, 2000) ?? "no details drawer",
        )
        .catch((e: unknown) => `no page: ${String(e)}`);
      console.log(`--- details drawer\n${drawer}`);
    }
    expect(
      problems,
      "page errors and Content Security Policy violations",
    ).toEqual([]);
  });

  test("1. sign in, paste a Vault token and see only the clusters it allows", async ({
    page,
  }) => {
    await signInWithToken(page, "e2e-rail");
    const rail = clusterRail(page);
    await expect(rail.getByRole("heading", { name: "Vault" })).toBeVisible();
    await expect(rail.getByRole("link", { name: "alpha" })).toBeVisible();
    await expect(rail.getByRole("link", { name: "beta" })).toHaveCount(0);
  });

  test("2. pods are listed live: a new pod appears and a deleted one disappears without a reload", async ({
    page,
  }) => {
    await signInWithToken(page, "e2e-pods");
    await openAlpha(page);
    await page
      .getByRole("navigation", { name: "Resources" })
      .getByRole("link", { name: "Pods" })
      .click();
    const grid = page.getByRole("grid", { name: "Pods" });
    await expect(grid.getByText(podOf("web"))).toBeVisible();

    const name = "e2e-live-pod";
    kubectl(
      "alpha",
      "-n",
      "demo",
      "delete",
      "pod",
      name,
      "--ignore-not-found",
      "--wait=true",
    );
    try {
      await page.getByRole("searchbox", { name: /Filter Pods/ }).fill(name);
      await expect(grid.getByText(name)).toHaveCount(0);
      kubectl(
        "alpha",
        "-n",
        "demo",
        "run",
        name,
        "--image=registry.k8s.io/pause:3.10",
        "--restart=Never",
      );
      await expect(grid.getByText(name)).toBeVisible();
      kubectl("alpha", "-n", "demo", "delete", "pod", name, "--wait=false");
      await expect(grid.getByText(name)).toHaveCount(0, { timeout: 60_000 });
    } finally {
      kubectl(
        "alpha",
        "-n",
        "demo",
        "delete",
        "pod",
        name,
        "--ignore-not-found",
        "--wait=false",
      );
    }
  });

  test("3. the logs of a pod show in the dock and the search highlights matches", async ({
    page,
  }) => {
    await signInWithToken(page, "e2e-logs");
    await openAlpha(page);
    const pod = podOf("logger");
    await page.goto(`/c/v%3Aalpha/ns/demo/core/Pod/${pod}`);
    const drawer = page.getByRole("complementary", { name: "Details" });
    await drawer.getByRole("tab", { name: "Logs" }).click();
    await drawer.getByRole("button", { name: `Open logs for ${pod}` }).click();

    const dock = page.getByRole("region", { name: "Dock" });
    await expect(dock.getByRole("tab", { name: `Logs: ${pod}` })).toBeVisible();
    await expect(dock.getByText("tick").first()).toBeVisible();
    await dock.getByLabel("Search").fill("tick");
    await expect(dock.locator("mark").first()).toHaveText("tick");
  });

  test("4. a shell in the shell pod runs a command in the terminal", async ({
    page,
  }) => {
    await signInWithToken(page, "e2e-shell");
    await openAlpha(page);
    await page.goto(`/c/v%3Aalpha/ns/demo/core/Pod/${podOf("shell")}`);
    await page.getByRole("button", { name: "Open shell" }).click();
    const dock = page.getByRole("region", { name: "Dock" });
    await expect(dock.getByRole("tab", { name: /Shell/ })).toBeVisible();
    const terminal = dock.locator(".xterm");
    await expect(terminal).toBeVisible();
    await terminal.click();
    await page.keyboard.type("echo e2e-$((40+2))\n");
    await expect(terminal).toContainText("e2e-42");
  });

  test("5. the YAML of a ConfigMap is edited in Monaco, previewed as a diff, saved and visible in the cluster", async ({
    page,
  }) => {
    await signInWithToken(page, "e2e-yaml");
    await openAlpha(page);
    await page.goto("/c/v%3Aalpha/ns/demo/core/ConfigMap/settings");
    const drawer = page.getByRole("complementary", { name: "Details" });
    await drawer.getByRole("tab", { name: "YAML" }).click();
    await expect.poll(() => editorText(page)).toContain("mode: integration");
    // The editor is read-only until the access check has answered, and keys typed before that are dropped.
    await expect(
      drawer.getByText("You can read this object but not change it."),
    ).toBeHidden();

    try {
      const lines = (await editorText(page)).split("\n");
      const index = lines.findIndex((l) => l.includes("mode: integration"));
      expect(index).toBeGreaterThanOrEqual(0);
      await page.locator(".monaco-editor").first().click();
      await page.keyboard.press("Control+Home");
      for (let i = 0; i < index; i++) await page.keyboard.press("ArrowDown");
      await page.keyboard.press("End");
      await page.keyboard.press("Shift+Home");
      await page.keyboard.type("mode: changed-by-e2e");
      // no Escape to close the completion popup: the drawer closes on Escape

      await drawer.getByRole("button", { name: "Preview" }).click();
      await expect(drawer.getByLabel("Preview of the change")).toContainText(
        "changed-by-e2e",
      );
      await drawer.getByRole("button", { name: "Save" }).click();
      await expect(drawer.getByText("Saved.")).toBeVisible();
      expect(
        kubectl(
          "alpha",
          "-n",
          "demo",
          "get",
          "configmap",
          "settings",
          "-o",
          "jsonpath={.data.mode}",
        ),
      ).toBe("changed-by-e2e");
    } finally {
      kubectl(
        "alpha",
        "-n",
        "demo",
        "patch",
        "configmap",
        "settings",
        "-p",
        '{"data":{"mode":"integration"}}',
      );
    }
  });

  test("6. a Secret value is masked until it is revealed", async ({ page }) => {
    await signInWithToken(page, "e2e-secret");
    await openAlpha(page);
    await page.goto("/c/v%3Aalpha/ns/demo/core/Secret/credentials");
    const data = page.getByRole("region", { name: "Secret data" });
    await expect(data.getByText("••••••••")).toBeVisible();
    await expect(page.getByText("s3cret-it-value")).toHaveCount(0);
    await data.getByRole("button", { name: "Reveal password" }).click();
    await expect(data.getByText("s3cret-it-value")).toBeVisible();
  });

  test("7. the Files tab of the shell pod lists the root directory", async ({
    page,
  }) => {
    await signInWithToken(page, "e2e-files");
    await openAlpha(page);
    await page.goto(`/c/v%3Aalpha/ns/demo/core/Pod/${podOf("shell")}`);
    const drawer = page.getByRole("complementary", { name: "Details" });
    await drawer.getByRole("tab", { name: "Files" }).click();
    const list = drawer.getByRole("table", { name: "Files" });
    await expect(
      list.getByRole("button", { name: "etc", exact: true }),
    ).toBeVisible();
    await expect(
      list.getByRole("button", { name: "bin", exact: true }),
    ).toBeVisible();
  });

  test("8. a reload keeps the session; after signing out and in again the saved token copy restores the clusters", async ({
    page,
  }) => {
    await signInWithToken(page, "e2e-reload");
    await page.reload();
    await expect(
      clusterRail(page).getByRole("link", { name: "alpha" }),
    ).toBeVisible();

    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(
      page.getByRole("link", { name: "Sign in with Google" }),
    ).toBeVisible();
    await signIn(page, "e2e-reload");
    // no token is pasted: the copy saved in this browser (IndexedDB) is sent back to the server
    await expect(
      clusterRail(page).getByRole("link", { name: "alpha" }),
    ).toBeVisible();
  });

  test("10. signing out shows the sign-in page", async ({ page }) => {
    await signInWithToken(page, "e2e-signout");
    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(
      page.getByRole("heading", { level: 1, name: "Kubelook" }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Sign in with Google" }),
    ).toBeVisible();
    await expect(clusterRail(page)).toHaveCount(0);
  });
});
