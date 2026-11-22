import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { expect, type Page } from "@playwright/test";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");

/** A Vault token written by hack/vault-setup.sh, e.g. token('ALICE_TOKEN'). */
export function token(name: string): string {
  for (const line of readFileSync(
    join(root, ".it", "tokens.env"),
    "utf8",
  ).split("\n")) {
    const [k, ...v] = line.split("=");
    if (k === name) return v.join("=");
  }
  throw new Error(`${name} is not in .it/tokens.env`);
}

/** Runs kubectl against a kind cluster ("alpha" or "beta") and returns its output. */
export function kubectl(cluster: string, ...args: string[]): string {
  return execFileSync(
    join(root, "bin", "kubectl"),
    [
      "--kubeconfig",
      join(root, ".it", `kubeconfig-${cluster}-host.yaml`),
      ...args,
    ],
    { encoding: "utf8" },
  ).trim();
}

/** The name of the running pod of a fixture Deployment in the "demo" namespace of alpha. */
export function podOf(app: string): string {
  return kubectl(
    "alpha",
    "-n",
    "demo",
    "get",
    "pods",
    "-l",
    `app=${app}`,
    "--field-selector=status.phase=Running",
    "-o",
    "jsonpath={.items[0].metadata.name}",
  );
}

/** Tells the identity stand-in which account signs in next. */
export async function announce(sub: string): Promise<void> {
  const res = await fetch("http://127.0.0.1:8300/admin/next", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      sub,
      email: `${sub}@corp.example`,
      hd: "corp.example",
    }),
  });
  expect(res.ok).toBe(true);
}

/** Signs in through the stand-in with Google. */
export async function signIn(page: Page, sub: string): Promise<void> {
  await announce(sub);
  await page.goto("/");
  await page.getByRole("link", { name: "Sign in with Google" }).click();
}

/** Signs in and pastes a Vault token; ends with the cluster rail showing the clusters of that token. */
export async function signInWithToken(
  page: Page,
  sub: string,
  name = "ALICE_TOKEN",
): Promise<void> {
  await signIn(page, sub);
  await expect(
    page.getByRole("heading", { name: "Add your credentials" }),
  ).toBeVisible();
  await page.getByLabel("Vault token", { exact: true }).fill(token(name));
  await page.getByRole("button", { name: "Add Vault token" }).click();
  await expect(
    clusterRail(page).getByRole("link", { name: "alpha" }),
  ).toBeVisible();
}

export function clusterRail(page: Page) {
  return page.getByRole("navigation", { name: "Clusters" });
}

/** Opens alpha from the rail (its first use mints the cluster credentials). */
export async function openAlpha(page: Page): Promise<void> {
  await clusterRail(page).getByRole("link", { name: "alpha" }).click();
  await expect(page).toHaveURL(/\/c\/v(:|%3A)alpha/);
}

/** Collects what a page must never do: uncaught errors and Content Security Policy violations. */
export function guardPage(page: Page): string[] {
  const problems: string[] = [];
  page.on("pageerror", (e) => problems.push(`page error: ${e.message}`));
  page.on("console", (m) => {
    if (m.text().includes("Content Security Policy"))
      problems.push(`console: ${m.text()}`);
  });
  return problems;
}

/** The text of the Monaco editor on the page (it draws text in `.view-line` elements, which have no role). */
export async function editorText(page: Page): Promise<string> {
  const lines = await page.locator(".monaco-editor .view-line").allInnerTexts();
  return lines.join("\n").replaceAll(" ", " ");
}
