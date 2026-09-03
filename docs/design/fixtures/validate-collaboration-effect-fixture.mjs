#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const fixturePath = process.argv[2] || 'docs/design/fixtures/collaboration-effect-mvp-v1.json';
const repoRoot = process.cwd();
const fixture = JSON.parse(fs.readFileSync(fixturePath, 'utf8'));

const failures = [];
let required = 0;
let covered = 0;

function check(cond, label) {
  required += 1;
  if (cond) {
    covered += 1;
  } else {
    failures.push(label);
  }
}

function has(obj, key) {
  return Object.prototype.hasOwnProperty.call(obj || {}, key);
}

function requireFields(fact, object, prefix, fields) {
  for (const field of fields) {
    check(has(object, field) && object[field] !== undefined && object[field] !== null && object[field] !== '', `${fact.id}: missing ${prefix}.${field}`);
  }
}

function requireCommon(fact) {
  requireFields(fact, fact, 'fact', ['id', 'event_type', 'occurred_at', 'actor_ref', 'refs', 'payload', 'expected_rule']);
  requireFields(fact, fact.refs, 'refs', ['project_id']);
  check(Array.isArray(fact.source_evidence) && fact.source_evidence.length > 0, `${fact.id}: missing source_evidence`);
  for (const [index, evidence] of (fact.source_evidence || []).entries()) {
    requireFields(fact, evidence, `source_evidence[${index}]`, ['kind', 'path', 'line', 'producer', 'proves']);
    check(['production_code', 'production_test', 'sanitized_historical_fixture'].includes(evidence.kind), `${fact.id}: invalid source_evidence kind ${evidence.kind}`);
    const evidencePath = path.join(repoRoot, evidence.path);
    check(fs.existsSync(evidencePath), `${fact.id}: source_evidence path does not exist: ${evidence.path}`);
    check(Number.isInteger(evidence.line) && evidence.line > 0, `${fact.id}: source_evidence line must be positive integer`);
    if (fs.existsSync(evidencePath) && Number.isInteger(evidence.line)) {
      const lineCount = fs.readFileSync(evidencePath, 'utf8').split('\n').length;
      check(evidence.line <= lineCount, `${fact.id}: source_evidence line ${evidence.line} exceeds ${evidence.path} line count ${lineCount}`);
    }
    check(Array.isArray(evidence.proves) && evidence.proves.length > 0, `${fact.id}: source_evidence proves must be non-empty`);
  }
}

function requireTaskPayload(fact, extra = []) {
  requireFields(fact, fact.refs, 'refs', ['task_id']);
  requireFields(fact, fact.payload, 'payload', ['task_id', 'project_id', 'owner_ref', 'status', 'effective_subscribers', ...extra]);
  check(Array.isArray(fact.payload.effective_subscribers) && fact.payload.effective_subscribers.length > 0, `${fact.id}: effective_subscribers must be non-empty array`);
}

function requireAuditPayload(fact, extra = []) {
  requireFields(fact, fact.payload, 'payload', ['audit_id', 'object_type', 'object_id', 'change_type', ...extra]);
}

for (const fact of fixture.facts || []) {
  requireCommon(fact);
  check(fact.event_type !== 'AgentTraceEvent' && !String(fact.event_type).includes('AgentTraceEvent'), `${fact.id}: AgentTraceEvent must not be used`);

  switch (fact.event_type) {
    case 'pm.task.assigned':
      requireTaskPayload(fact, ['assignee']);
      check(String(fact.payload.assignee).startsWith('agent:') || fact.expected_rule === 'NONE', `${fact.id}: agent assignment rule must fail closed for non-agent assignee`);
      break;
    case 'pm.task.reassigned':
      requireTaskPayload(fact, ['assignee', 'previous_assignee']);
      break;
    case 'pm.task.state_changed':
      requireTaskPayload(fact, ['assignee', 'prev_status']);
      break;
    case 'pm.audit_recorded':
      requireAuditPayload(fact);
      if (fact.payload.change_type === 'status_changed') {
        requireFields(fact, fact.payload, 'payload', ['field', 'from_value', 'to_value']);
      }
      if (fact.payload.change_type === 'review_verdict') {
        requireFields(fact, fact.payload, 'payload', ['field', 'to_value', 'detail']);
        requireFields(fact, fact.payload.detail, 'payload.detail', ['blocking', 'reason', 'round', 'plan_id']);
      }
      if (fact.payload.change_type === 'dependency_added') {
        requireFields(fact, fact.payload, 'payload', ['detail']);
        requireFields(fact, fact.payload.detail, 'payload.detail', ['from', 'to']);
      }
      if (fact.payload.change_type === 'dependency_removed') {
        requireFields(fact, fact.payload, 'payload', ['detail']);
        check(fact.expected_rule === 'NONE', `${fact.id}: dependency_removed must not project R6_DEP_RELEASE`);
      }
      break;
    case 'conversation.message_added':
      requireFields(fact, fact.refs, 'refs', ['conversation_id', 'message_id']);
      requireFields(fact, fact.payload, 'payload', ['conversation_id', 'message_id', 'sender', 'owner_ref']);
      check(fact.expected_rule === 'NONE', `${fact.id}: conversation message alone must remain neutral/non-effect`);
      break;
    default:
      failures.push(`${fact.id}: unknown event_type ${fact.event_type}`);
  }

  if (fact.expected_rule === 'R6_DEP_RELEASE') {
    check(Array.isArray(fact.paired_with) && fact.paired_with.length > 0, `${fact.id}: R6_DEP_RELEASE requires paired dependency_added evidence`);
    for (const pairID of fact.paired_with || []) {
      const pair = fixture.facts.find((candidate) => candidate.id === pairID);
      check(pair?.payload?.change_type === 'dependency_added', `${fact.id}: R6 pair ${pairID} must be dependency_added`);
    }
  }
}

const auditedRequired = required;
const auditedCovered = covered;
const auditedRatio = Number((auditedCovered / auditedRequired).toFixed(4));

check((fixture.facts || []).length >= 20, 'fixture must contain at least 20 audited rows');
check(fixture.field_coverage_audit?.sample_count === (fixture.facts || []).length, 'field_coverage_audit.sample_count mismatch');
check(fixture.field_coverage_audit?.required_field_checks === auditedRequired, `field_coverage_audit.required_field_checks mismatch: got ${fixture.field_coverage_audit?.required_field_checks}, want ${auditedRequired}`);
check(fixture.field_coverage_audit?.covered_field_checks === auditedCovered, `field_coverage_audit.covered_field_checks mismatch: got ${fixture.field_coverage_audit?.covered_field_checks}, want ${auditedCovered}`);
check(fixture.field_coverage_audit?.coverage_ratio === auditedRatio, `field_coverage_audit.coverage_ratio mismatch: got ${fixture.field_coverage_audit?.coverage_ratio}, want ${auditedRatio}`);

const result = {
  sample_count: (fixture.facts || []).length,
  required_field_checks: auditedRequired,
  covered_field_checks: auditedCovered,
  coverage_ratio: auditedRatio,
  failures,
};

console.log(JSON.stringify(result, null, 2));
if (failures.length > 0) {
  process.exit(1);
}
