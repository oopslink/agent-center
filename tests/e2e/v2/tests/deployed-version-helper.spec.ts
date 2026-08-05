import { test, expect } from "@playwright/test";

import {
  assertVersionChecks,
  expectedWorkerReportedVersion,
  parseAgentCenterVersionOutput,
  type BinaryIdentity,
  type CurrentArtifact,
} from "../helpers/deployed-version.js";

function identity(version: string, commit: string, buildID: string, realPath: string): BinaryIdentity {
  return {
    path: realPath,
    realPath,
    version,
    commit,
    buildID,
    mtime: "2026-08-05T00:00:00.000Z",
  };
}

function currentFor(binary: BinaryIdentity): CurrentArtifact {
  return {
    prefix: "/tmp/ac-smoke",
    currentLink: "/tmp/ac-smoke/current",
    target: "/tmp/ac-smoke/versions/" + binary.version,
    targetRealPath: "/tmp/ac-smoke/versions/" + binary.version,
    versionMarker: binary.version,
    commitMarker: binary.commit,
    binary,
  };
}

test.describe("deployed binary version assertion helper", () => {
  test("consistent center + worker + agent-runtime state passes", () => {
    const expected = identity("v2.30.0", "abc1234", "build-new", "/tmp/ac-smoke/versions/v2.30.0/bin/agent-center");
    const current = currentFor(expected);

    expect(() =>
      assertVersionChecks([
        {
          component: "center",
          expected,
          running: expected,
          current,
          process: { pid: 1001, ppid: 1, startedAt: "Wed Aug  5 12:00:00 2026", exePath: expected.realPath },
          reported: { source: "center /api/system/version", version: expected.version, commit: expected.commit, builtAt: "2026-08-05T11:59:00Z" },
        },
        {
          component: "worker",
          expected,
          running: expected,
          current,
          process: { pid: 1002, ppid: 1001, startedAt: "Wed Aug  5 12:00:01 2026", exePath: expected.realPath },
          reported: { source: "admin worker/find-all", version: expectedWorkerReportedVersion(expected) },
          expectedReportedVersion: expectedWorkerReportedVersion(expected),
        },
        {
          component: "agent-runtime",
          agentID: "agent-ok",
          expected,
          running: expected,
          current,
          process: { pid: 1003, ppid: 1002, startedAt: "Wed Aug  5 12:00:02 2026", exePath: expected.realPath },
          runtimeParent: "worker",
          adopted: false,
          pidStorePath: "/tmp/acw-ok/agent-pids.json",
        },
      ]),
    ).not.toThrow();
  });

  test("old adopted agent-runtime skew fails with locatable diagnostics", () => {
    const expected = identity("v2.30.0", "new1234", "build-new", "/tmp/ac-smoke/versions/v2.30.0/bin/agent-center");
    const old = identity("v2.29.0", "old9876", "build-old", "/tmp/ac-smoke/versions/v2.29.0/bin/agent-center");
    const current = currentFor(expected);

    let message = "";
    try {
      assertVersionChecks([
        {
          component: "agent-runtime",
          agentID: "agent-skew",
          expected,
          running: old,
          current,
          process: {
            pid: 4242,
            ppid: 1,
            startedAt: "Wed Aug  5 11:55:00 2026",
            exePath: old.realPath,
          },
          runtimeParent: "adopted-orphan",
          adopted: true,
          pidStorePath: "/tmp/acw-skew/agent-pids.json",
        },
      ]);
    } catch (err) {
      message = String((err as Error).message);
    }

    expect(message).toContain("deployed-binary runtime version consistency failed");
    expect(message).toContain("component=agent-runtime");
    expect(message).toContain("agent_id=agent-skew");
    expect(message).toContain("expected version=v2.30.0");
    expect(message).toContain("running version=v2.29.0");
    expect(message).toContain("expected commit=new1234");
    expect(message).toContain("running commit=old9876");
    expect(message).toContain("pid=4242");
    expect(message).toContain("started_at=");
    expect(message).toContain("runtime_parent=adopted-orphan");
    expect(message).toContain("adopt=adopted");
    expect(message).toContain("pid_store=");
    expect(message).toContain("reason=version_mismatch");
  });

  test("parses agent-center version command output", () => {
    expect(parseAgentCenterVersionOutput("agent-center v2.30.0 (commit abc1234)\n")).toEqual({
      version: "v2.30.0",
      commit: "abc1234",
    });
  });
});
