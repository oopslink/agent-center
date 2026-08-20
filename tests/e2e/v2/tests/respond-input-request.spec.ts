import { test, expect } from "../fixtures/agent-center.js";

type ProjectDTO = { id: string };
type TaskDTO = { id: string; status: string; blocked_reason?: string };

async function createProjectAndTask(
  request: import("@playwright/test").APIRequestContext,
  orgApiURL: string,
): Promise<{ projectID: string; taskID: string }> {
  const projectResp = await request.post(orgApiURL + "/projects", {
    data: { name: "Input Response Demo", description: "current PM task flow" },
  });
  expect(projectResp.status(), `project create: ${await projectResp.text()}`).toBe(200);
  const project = (await projectResp.json()) as ProjectDTO;

  const taskResp = await request.post(`${orgApiURL}/projects/${project.id}/tasks`, {
    data: { title: "Q1 audit", description: "needs human input" },
  });
  expect(taskResp.status(), `task create: ${await taskResp.text()}`).toBe(200);
  const task = (await taskResp.json()) as TaskDTO;
  return { projectID: project.id, taskID: task.id };
}

test.describe("input-response task flow", () => {
  test("blocked task → unblock response clears blocked state", async ({
    request,
    authSession,
  }) => {
    const { projectID, taskID } = await createProjectAndTask(request, authSession.orgApiURL);
    const taskBase = `${authSession.orgApiURL}/projects/${projectID}/tasks/${taskID}`;

    const start = await request.post(taskBase + "/start");
    expect(start.status(), `start: ${await start.text()}`).toBe(200);

    const block = await request.post(taskBase + "/block", {
      data: { reason: "Approve audit scope?" },
    });
    expect(block.status(), `block: ${await block.text()}`).toBe(200);
    const blocked = (await block.json()) as TaskDTO;
    expect(blocked.status).toBe("blocked");

    const unblock = await request.post(taskBase + "/unblock", {
      data: { comment: "yes - proceed" },
    });
    expect(unblock.status(), `unblock: ${await unblock.text()}`).toBe(200);
    const unblocked = (await unblock.json()) as TaskDTO;
    expect(unblocked.status).not.toBe("blocked");

    const get = await request.get(taskBase);
    expect(get.status(), `get task: ${await get.text()}`).toBe(200);
    const current = (await get.json()) as TaskDTO;
    expect(current.status).toBe(unblocked.status);
    expect(current.blocked_reason ?? "").toBe("");
  });

  test("error path: unblock nonexistent task → 404 not_found", async ({
    request,
    authSession,
  }) => {
    const projectResp = await request.post(authSession.orgApiURL + "/projects", {
      data: { name: "Input Response Errors", description: "current PM task flow" },
    });
    expect(projectResp.status(), `project create: ${await projectResp.text()}`).toBe(200);
    const project = (await projectResp.json()) as ProjectDTO;
    const r = await request.post(
      `${authSession.orgApiURL}/projects/${project.id}/tasks/task-does-not-exist/unblock`,
      { data: { comment: "x" } },
    );
    expect(r.status()).toBe(404);
    const body = (await r.json()) as { error?: string };
    expect(body.error).toBe("not_found");
  });
});
