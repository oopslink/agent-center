import { expect, test } from "@playwright/test";

import {
  assertRuntimeVersionConsistency,
  type ArtifactExpectation,
  type VersionObservation,
} from "../helpers/runtime-version.js";

const baseExpected: ArtifactExpectation = {
  binaryPath: "/opt/ac/current/bin/agent-center",
  binaryRealPath: "/opt/ac/versions/hotfix-new/bin/agent-center",
  version: "hotfix-new-def456",
  commit: "def456",
  workerVersion: "hotfix-new-def456+def456",
  buildID: "build-new",
  artifactMtimeMs: Date.parse("2026-08-06T10:00:00Z"),
  activatedAtMs: Date.parse("2026-08-06T10:00:02Z"),
  currentLink: "/opt/ac/current",
  currentTarget: "/opt/ac/versions/hotfix-new",
};

test.describe("runtime version smoke helper", () => {
  test("consistent center worker and agent-runtime observations pass", () => {
    const observations: VersionObservation[] = [
      {
        component: "center",
        pid: 101,
        startedAtMs: Date.parse("2026-08-06T10:00:03Z"),
        startedAt: "Thu Aug 6 10:00:03 2026",
        version: "hotfix-new-def456",
        commit: "def456",
        buildID: "build-new",
        executablePath: "/opt/ac/current/bin/agent-center",
      },
      {
        component: "worker",
        pid: 202,
        startedAtMs: Date.parse("2026-08-06T10:00:04Z"),
        startedAt: "Thu Aug 6 10:00:04 2026",
        version: "hotfix-new-def456+def456",
        commit: "def456",
        buildID: "build-new",
        executablePath: "/opt/ac/current/bin/agent-center",
      },
      {
        component: "agent-runtime",
        pid: 303,
        parentPID: 202,
        startedAtMs: Date.parse("2026-08-06T10:00:05Z"),
        startedAt: "2026-08-06T10:00:05Z",
        version: "hotfix-new-def456+def456",
        commit: "def456",
        buildID: "build-new",
        executablePath: "/opt/ac/current/bin/agent-center",
      },
    ];

    expect(() =>
      assertRuntimeVersionConsistency(baseExpected, observations, {
        workerPid: 202,
        workerStartedAtMs: Date.parse("2026-08-06T10:00:04Z"),
      }),
    ).not.toThrow();
  });

  test("old adopted agent-runtime skew fails with actionable diagnostics", () => {
    const observations: VersionObservation[] = [
      {
        component: "center",
        pid: 101,
        startedAtMs: Date.parse("2026-08-06T10:00:03Z"),
        startedAt: "Thu Aug 6 10:00:03 2026",
        version: "hotfix-new-def456",
        commit: "def456",
        buildID: "build-new",
        executablePath: "/opt/ac/current/bin/agent-center",
      },
      {
        component: "worker",
        pid: 202,
        startedAtMs: Date.parse("2026-08-06T10:00:04Z"),
        startedAt: "Thu Aug 6 10:00:04 2026",
        version: "hotfix-new-def456+def456",
        commit: "def456",
        buildID: "build-new",
        executablePath: "/opt/ac/current/bin/agent-center",
      },
      {
        component: "agent-runtime",
        pid: 303,
        parentPID: 1,
        startedAtMs: Date.parse("2026-08-06T09:59:55Z"),
        startedAt: "2026-08-06T09:59:55Z",
        version: "hotfix-old-abc123+abc123",
        commit: "abc123",
        buildID: "build-old",
        executablePath: "/opt/ac/versions/hotfix-old/bin/agent-center",
      },
    ];

    expect(() =>
      assertRuntimeVersionConsistency(baseExpected, observations, {
        workerPid: 202,
        workerStartedAtMs: Date.parse("2026-08-06T10:00:04Z"),
      }),
    ).toThrow(
      /agent-runtime: version mismatch; .*expected_version=hotfix-new-def456\+def456.*running_version=hotfix-old-abc123\+abc123.*pid=303.*parent_pid=1.*adopted=true/,
    );
  });
});
