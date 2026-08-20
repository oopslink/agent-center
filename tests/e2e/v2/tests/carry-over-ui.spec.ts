import { test, expect } from "../fixtures/agent-center.js";

// The legacy flat derive-from-message route is retired. This browser test keeps
// coverage on the current PM issue route and verifies the created work item is
// reachable through the org-scoped UI.
test.describe("Project issue UI", () => {
  test("create PM issue through current API → navigate to issue detail", async ({
    page,
    request,
    agentCenter,
    authSession,
  }) => {
    const projectResp = await request.post(authSession.orgApiURL + "/projects", {
      data: { name: "Issue UI Demo", description: "current PM route" },
    });
    expect(projectResp.status(), `project create: ${await projectResp.text()}`).toBe(200);
    const project = (await projectResp.json()) as { id: string };

    const issueResp = await request.post(`${authSession.orgApiURL}/projects/${project.id}/issues`, {
      data: { title: "Investigate review room", description: "Created through PM issue API" },
    });
    expect(issueResp.status(), `issue create: ${await issueResp.text()}`).toBe(200);
    const issue = (await issueResp.json()) as { id: string; title: string };

    await page.goto(
      `${agentCenter.baseURL}/organizations/${authSession.orgSlug}/projects/${project.id}/issues/${issue.id}`,
    );
    await expect(page.getByTestId("page-IssueDetail")).toBeVisible();
    await expect(page.getByRole("heading", { name: new RegExp(issue.title) })).toBeVisible();
  });
});
