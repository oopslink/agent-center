import { test, expect } from "../fixtures/agent-center.js";
import type { APIRequestContext } from "@playwright/test";

type SignupResponse = {
  identity_id: string;
  organization_slug: string;
};

type InvitationResponse = {
  token: string;
};

async function signup(
  request: APIRequestContext,
  baseURL: string,
  displayName: string,
  passcode: string,
  email: string,
  organizationName: string,
): Promise<SignupResponse> {
  const response = await request.post(baseURL + "/api/auth/signup", {
    data: {
      display_name: displayName,
      passcode,
      email,
      organization_name: organizationName,
    },
  });
  if (response.status() !== 201) {
    throw new Error(`signup failed: ${response.status()} ${await response.text()}`);
  }
  return (await response.json()) as SignupResponse;
}

test.describe("AI Runtime deployed-binary browser acceptance", () => {
  test("owner can edit/import while member is read-only and System nav owns AI Runtime", async ({
    browser,
    context,
    page,
    agentCenter,
  }) => {
    test.setTimeout(60_000);
    const suffix = Math.random().toString(36).slice(2, 8);
    const ownerPasscode = "OwnerPass1!";
    const memberPasscode = "MemberPass1!";

    const owner = await signup(
      context.request,
      agentCenter.baseURL,
      `owner-${suffix}`,
      ownerPasscode,
      `owner-${suffix}@example.com`,
      `Runtime Org ${suffix}`,
    );

    const memberContext = await browser.newContext();
    try {
      const member = await signup(
        memberContext.request,
        agentCenter.baseURL,
        `member-${suffix}`,
        memberPasscode,
        `member-${suffix}@example.com`,
        `Member Org ${suffix}`,
      );

      const invite = await context.request.post(
        `${agentCenter.apiURL}/orgs/${owner.organization_slug}/invitations`,
        { data: { invitee_user_id: member.identity_id, role: "member" } },
      );
      if (invite.status() !== 201) {
        throw new Error(`invite failed: ${invite.status()} ${await invite.text()}`);
      }
      const invitation = (await invite.json()) as InvitationResponse;
      const accept = await memberContext.request.post(
        `${agentCenter.apiURL}/orgs/${owner.organization_slug}/invitations/${invitation.token}/accept`,
      );
      if (accept.status() !== 200) {
        throw new Error(`accept failed: ${accept.status()} ${await accept.text()}`);
      }

      await page.goto(`${agentCenter.baseURL}/ai-runtime`);
      await expect(page).toHaveURL(new RegExp(`/organizations/${owner.organization_slug}/ai-runtime$`));

      await page.getByTestId("rail-module-system").click();
      await expect(page.getByTestId("page-Environment")).toBeVisible();
      const systemNav = page.getByRole("navigation", { name: /^primary$/ });
      const orderedSystemItems = await systemNav.locator("a").evaluateAll((links) =>
        links.map((link) => link.getAttribute("data-testid")).filter(Boolean),
      );
      expect(orderedSystemItems.indexOf("system-nav-environment")).toBeLessThan(
        orderedSystemItems.indexOf("system-nav-ai-runtime"),
      );
      expect(orderedSystemItems.indexOf("system-nav-ai-runtime")).toBeLessThan(
        orderedSystemItems.indexOf("system-nav-settings"),
      );

      await page.getByTestId("system-nav-ai-runtime").click();
      await expect(page.getByTestId("page-AiRuntime")).toBeVisible();
      await expect(page.getByTestId("system-nav-ai-runtime")).toHaveAttribute("aria-current", "page");

      await page.getByTestId("ai-runtime-tab-models").click();
      await page.getByTestId("ai-runtime-import-models").click();
      await page.getByTestId("ai-runtime-model-import-json").fill(
        JSON.stringify([
          {
            key: "gpt-browser",
            model_key: "gpt-browser",
            display_name: "GPT Browser",
            compatible_cli_keys: ["codex"],
            enabled: true,
            context_window: 128000,
            input_cost_per_mtok: 0.2,
            output_cost_per_mtok: 0.8,
            tier: "standard",
          },
        ]),
      );
      await page.getByTestId("ai-runtime-model-import-preview-btn").click();
      await expect(page.getByTestId("ai-runtime-model-import-change").filter({ hasText: "gpt-browser" })).toBeVisible();
      await expect(page.getByTestId("ai-runtime-model-import-scope")).toContainText("Profiles");
      await page.getByTestId("ai-runtime-model-import-apply").click();
      await expect(page.getByTestId("ai-runtime-model-import-applied")).toContainText("New revision");
      await page.getByRole("button", { name: "Close" }).click();

      const importedModel = page.getByTestId("ai-runtime-model-row").filter({ hasText: "GPT Browser" });
      await expect(importedModel).toBeVisible();
      await importedModel.getByTestId("ai-runtime-edit-model").click();
      await page.getByTestId("ai-runtime-model-display-name").fill("GPT Browser Updated");
      await page.getByTestId("ai-runtime-form-save").click();
      await expect(page.getByTestId("ai-runtime-model-row").filter({ hasText: "GPT Browser Updated" })).toBeVisible();

      await page.getByTestId("ai-runtime-tab-profiles").click();
      await page.getByTestId("ai-runtime-create-profile").click();
      await page.getByTestId("ai-runtime-profile-key").fill("browser-profile");
      await page.getByTestId("ai-runtime-profile-name").fill("Browser profile");
      await page.getByTestId("ai-runtime-form-save").click();
      await expect(page.getByTestId("ai-runtime-profile-row").filter({ hasText: "Browser profile" })).toBeVisible();
      await test.info().attach("ai-runtime-owner-edit-import.png", {
        body: await page.screenshot({ fullPage: true }),
        contentType: "image/png",
      });

      const memberPage = await memberContext.newPage();
      await memberPage.goto(`${agentCenter.baseURL}/organizations/${owner.organization_slug}/ai-runtime`);
      await expect(memberPage.getByTestId("page-AiRuntime")).toBeVisible();
      await expect(memberPage.getByTestId("ai-runtime-permission")).toHaveAttribute("data-can-manage", "false");
      await expect(memberPage.getByTestId("ai-runtime-create-profile")).toHaveCount(0);
      await expect(memberPage.getByTestId("ai-runtime-edit-profile")).toHaveCount(0);
      await memberPage.getByTestId("ai-runtime-tab-models").click();
      await expect(memberPage.getByTestId("ai-runtime-import-models")).toHaveCount(0);
      await expect(memberPage.getByTestId("ai-runtime-edit-model")).toHaveCount(0);
      await memberPage.getByTestId("ai-runtime-tab-clis").click();
      await expect(memberPage.getByTestId("ai-runtime-create-cli")).toHaveCount(0);
      await expect(memberPage.getByTestId("ai-runtime-edit-cli")).toHaveCount(0);
      await test.info().attach("ai-runtime-member-read-only.png", {
        body: await memberPage.screenshot({ fullPage: true }),
        contentType: "image/png",
      });
    } finally {
      await memberContext.close();
    }
  });
});
