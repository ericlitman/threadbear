#!/usr/bin/env python3
"""Validate ThreadBear's public experiment registry and negative fixtures."""

from __future__ import annotations

import argparse
import copy
import datetime as dt
import json
import pathlib
import re
import sys
from typing import Any


ROOT = pathlib.Path(__file__).resolve().parents[1]
DEFAULT_REGISTRY = ROOT / "docs" / "experiments" / "registry.json"
DEFAULT_FIXTURES = ROOT / "scripts" / "fixtures" / "experiments-negative.json"

EXPERIMENT_ID = re.compile(r"^TB-EXP-[0-9]{4}-[0-9]{3}$")
CAPABILITY_ID = re.compile(r"^TB-CAP-[A-Z0-9-]+$")
PREFLIGHT_ID = re.compile(r"^TB-PRE-[0-9]{4}-[0-9]{3}$")
ISSUE_ID = re.compile(r"^BEAR-[0-9]+$")
EVIDENCE = re.compile(
    r"^(?:"
    r"linear:BEAR-[0-9]+(?:#(?:description|comment-[A-Za-z0-9-]+))?"
    r"|github-pr:[0-9]+"
    r"|git:[0-9a-f]{40}"
    r"|codex-(?:task|rollout):[0-9a-f-]{36}"
    r")$"
)
FORBIDDEN_PRIVACY_KEYS = {"prompt", "message_body", "screenshot", "user_email"}
FORBIDDEN_PRIVACY_TEXT = (
    "/users/",
    "file://",
    "<codex_delegation",
    "<recommended_plugins",
    ".png",
    ".jpg",
    ".jpeg",
    ".heic",
)
BARE_UNKNOWN = re.compile(r"^unknown\s*:?\s*$", re.IGNORECASE)
FIXTURE_MUTATIONS = {
    "bare-unknown",
    "capability-evidence-overlap",
    "closed-preflight-missing-result",
    "closed-preflight-wrong-result",
    "completed-next-preflight",
    "duplicate-experiment-id",
    "duplicate-json-key",
    "empty-changed-variable",
    "forbidden-privacy-key",
    "forbidden-privacy-text",
    "invalid-conflict-link",
    "missing-evidence",
    "pending-preflight-with-result",
    "shared-next-preflight",
}


class RegistryError(ValueError):
    pass


def reject_duplicate_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise RegistryError(f'duplicate JSON key "{key}"')
        result[key] = value
    return result


def load_json(path: pathlib.Path) -> Any:
    try:
        text = path.read_text()
    except OSError as error:
        raise RegistryError(f"{path}: {error}") from error
    return load_json_text(text, str(path))


def load_json_text(text: str, label: str) -> Any:
    try:
        return json.loads(text, object_pairs_hook=reject_duplicate_keys)
    except (json.JSONDecodeError, RegistryError) as error:
        raise RegistryError(f"{label}: {error}") from error


def exact_object(value: Any, keys: set[str], label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise RegistryError(f"{label} must be an object")
    missing = keys - value.keys()
    extra = value.keys() - keys
    if missing:
        raise RegistryError(f"{label} missing keys: {', '.join(sorted(missing))}")
    if extra:
        raise RegistryError(f"{label} has unknown keys: {', '.join(sorted(extra))}")
    return value


def nonempty_string(value: Any, label: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise RegistryError(f"{label} must be a non-empty string")
    return value


def recorded_string(value: Any, label: str) -> str:
    result = nonempty_string(value, label)
    if BARE_UNKNOWN.fullmatch(result):
        raise RegistryError(f"{label} must use 'unknown: <reason>'")
    return result


def string_list(value: Any, label: str, *, minimum: int = 0) -> list[str]:
    if not isinstance(value, list) or len(value) < minimum:
        raise RegistryError(f"{label} must contain at least {minimum} value(s)")
    for index, item in enumerate(value):
        nonempty_string(item, f"{label}[{index}]")
    if len(value) != len(set(value)):
        raise RegistryError(f"{label} contains duplicate values")
    return value


def validate_privacy(value: Any, path: str = "registry") -> None:
    if isinstance(value, dict):
        for key, child in value.items():
            if key.lower() in FORBIDDEN_PRIVACY_KEYS:
                raise RegistryError(f"{path} contains forbidden privacy key {key!r}")
            validate_privacy(child, f"{path}.{key}")
    elif isinstance(value, list):
        for index, child in enumerate(value):
            validate_privacy(child, f"{path}[{index}]")
    elif isinstance(value, str):
        lowered = value.lower()
        for fragment in FORBIDDEN_PRIVACY_TEXT:
            if fragment in lowered:
                raise RegistryError(f"{path} contains forbidden public-registry material {fragment!r}")


def validate_registry(registry: Any) -> None:
    top = exact_object(
        registry,
        {"schema_version", "updated_at", "canonical_for", "capabilities", "experiments", "preflights"},
        "registry",
    )
    if top["schema_version"] != 1 or isinstance(top["schema_version"], bool):
        raise RegistryError("schema_version must equal integer 1")
    try:
        dt.date.fromisoformat(nonempty_string(top["updated_at"], "updated_at"))
    except ValueError as error:
        raise RegistryError("updated_at must be an ISO date") from error
    nonempty_string(top["canonical_for"], "canonical_for")
    validate_privacy(top)

    experiments = top["experiments"]
    if not isinstance(experiments, list) or not experiments:
        raise RegistryError("experiments must contain at least one record")
    by_experiment: dict[str, dict[str, Any]] = {}
    for index, raw in enumerate(experiments):
        label = f"experiments[{index}]"
        item = exact_object(
            raw,
            {
                "id",
                "date",
                "issue",
                "preflight_id",
                "question",
                "invariant",
                "environment",
                "invocation",
                "evidence",
                "result",
                "confidence",
                "applicability",
                "supersedes",
                "conflicts",
            },
            label,
        )
        experiment_id = nonempty_string(item["id"], f"{label}.id")
        if not EXPERIMENT_ID.fullmatch(experiment_id):
            raise RegistryError(f"{label}.id has invalid experiment ID {experiment_id!r}")
        if experiment_id in by_experiment:
            raise RegistryError(f"duplicate experiment id {experiment_id!r}")
        by_experiment[experiment_id] = item
        try:
            dt.date.fromisoformat(nonempty_string(item["date"], f"{label}.date"))
        except ValueError as error:
            raise RegistryError(f"{label}.date must be an ISO date") from error
        if not ISSUE_ID.fullmatch(nonempty_string(item["issue"], f"{label}.issue")):
            raise RegistryError(f"{label}.issue must be a BEAR issue ID")
        preflight_id = item["preflight_id"]
        if preflight_id is not None and (
            not isinstance(preflight_id, str) or not PREFLIGHT_ID.fullmatch(preflight_id)
        ):
            raise RegistryError(f"{label}.preflight_id must be null or a preflight ID")
        for field in ("question", "invariant", "applicability"):
            nonempty_string(item[field], f"{label}.{field}")
        environment = exact_object(
            item["environment"],
            {
                "threadbear_version",
                "git_sha",
                "codex_version",
                "codex_source",
                "host",
                "task_state",
                "restart_state",
                "hook_fingerprint",
                "guidance_fingerprint",
            },
            f"{label}.environment",
        )
        for key, value in environment.items():
            recorded_string(value, f"{label}.environment.{key}")
        invocation = exact_object(
            item["invocation"],
            {"outer_tool", "code", "native_tool_identity", "target_identity_mode"},
            f"{label}.invocation",
        )
        for key, value in invocation.items():
            recorded_string(value, f"{label}.invocation.{key}")
        result = exact_object(
            item["result"],
            {"status", "summary", "timing_ms", "hook_participation", "rendered_proof"},
            f"{label}.result",
        )
        for key, value in result.items():
            recorded_string(value, f"{label}.result.{key}")
        evidence = string_list(item["evidence"], f"{label}.evidence", minimum=1)
        for reference in evidence:
            if not EVIDENCE.fullmatch(reference):
                raise RegistryError(f"{label}.evidence has invalid reference {reference!r}")
        if item["confidence"] not in {"high", "medium", "low"}:
            raise RegistryError(f"{label}.confidence must be high, medium, or low")
        string_list(item["supersedes"], f"{label}.supersedes")
        string_list(item["conflicts"], f"{label}.conflicts")

    for item in experiments:
        for relation_name in ("supersedes", "conflicts"):
            for target in item[relation_name]:
                if target not in by_experiment:
                    raise RegistryError(f"experiment {item['id']} {relation_name} references unknown experiment {target}")
                if target == item["id"]:
                    raise RegistryError(f"experiment {item['id']} cannot {relation_name} itself")
        for target in item["conflicts"]:
            if item["id"] not in by_experiment[target]["conflicts"]:
                raise RegistryError(f"conflict {item['id']} <-> {target} must be symmetric")

    preflights = top["preflights"]
    if not isinstance(preflights, list):
        raise RegistryError("preflights must be an array")
    by_preflight: dict[str, dict[str, Any]] = {}
    used_result_ids: set[str] = set()
    for index, raw in enumerate(preflights):
        label = f"preflights[{index}]"
        item = exact_object(
            raw,
            {
                "id",
                "issue",
                "capability_id",
                "status",
                "consulted",
                "remaining_unknown",
                "single_changed_variable",
                "held_constant",
                "predicted_outcomes",
                "stop_condition",
                "result_experiment_id",
            },
            label,
        )
        preflight_id = nonempty_string(item["id"], f"{label}.id")
        if not PREFLIGHT_ID.fullmatch(preflight_id) or preflight_id in by_preflight:
            raise RegistryError(f"invalid or duplicate preflight id {preflight_id!r}")
        by_preflight[preflight_id] = item
        if not ISSUE_ID.fullmatch(nonempty_string(item["issue"], f"{label}.issue")):
            raise RegistryError(f"{label}.issue must be a BEAR issue ID")
        capability_id = nonempty_string(item["capability_id"], f"{label}.capability_id")
        if not CAPABILITY_ID.fullmatch(capability_id):
            raise RegistryError(f"{label}.capability_id must be a capability ID")
        if item["status"] not in {"pending", "closed"}:
            raise RegistryError(f"{label}.status must be pending or closed")
        for consulted in string_list(item["consulted"], f"{label}.consulted", minimum=1):
            if consulted not in by_experiment:
                raise RegistryError(f"{label}.consulted references unknown experiment {consulted}")
        for field in ("remaining_unknown", "single_changed_variable", "held_constant", "stop_condition"):
            nonempty_string(item[field], f"{label}.{field}")
        string_list(item["predicted_outcomes"], f"{label}.predicted_outcomes", minimum=2)
        result_id = item["result_experiment_id"]
        if not isinstance(result_id, str):
            raise RegistryError(f"{label}.result_experiment_id must be a string")
        if item["status"] == "closed":
            result = by_experiment.get(result_id)
            if result is None:
                raise RegistryError(f"closed {label} must reference a result experiment")
            if result["issue"] != item["issue"]:
                raise RegistryError(f"closed {label} result must belong to the same issue")
            if result["preflight_id"] != preflight_id:
                raise RegistryError(f"closed {label} result must link back to {preflight_id}")
            if result_id in item["consulted"]:
                raise RegistryError(f"closed {label} cannot reuse a consulted experiment as its result")
            if result_id in used_result_ids:
                raise RegistryError(f"result experiment {result_id} closes more than one preflight")
            used_result_ids.add(result_id)
        if item["status"] == "pending" and result_id:
            raise RegistryError(f"pending {label} cannot reference a result experiment")

    for experiment in experiments:
        preflight_id = experiment["preflight_id"]
        if preflight_id is None:
            continue
        preflight = by_preflight.get(preflight_id)
        if preflight is None:
            raise RegistryError(f"experiment {experiment['id']} references unknown preflight {preflight_id}")
        if preflight["status"] != "closed" or preflight["result_experiment_id"] != experiment["id"]:
            raise RegistryError(f"experiment {experiment['id']} is not the declared result of {preflight_id}")

    capabilities = top["capabilities"]
    if not isinstance(capabilities, list) or not capabilities:
        raise RegistryError("capabilities must contain at least one record")
    capability_ids: set[str] = set()
    claimed_next_preflights: set[str] = set()
    for index, raw in enumerate(capabilities):
        label = f"capabilities[{index}]"
        item = exact_object(
            raw,
            {"id", "premise", "status", "supported_by", "contradicted_by", "decision", "next_preflight"},
            label,
        )
        capability_id = nonempty_string(item["id"], f"{label}.id")
        if not CAPABILITY_ID.fullmatch(capability_id) or capability_id in capability_ids:
            raise RegistryError(f"invalid or duplicate capability id {capability_id!r}")
        capability_ids.add(capability_id)
        for field in ("premise", "decision"):
            nonempty_string(item[field], f"{label}.{field}")
        if item["status"] not in {"established", "conditional", "unresolved", "rejected"}:
            raise RegistryError(f"{label}.status is invalid")
        evidence_sets: dict[str, set[str]] = {}
        for field in ("supported_by", "contradicted_by"):
            evidence_ids = string_list(item[field], f"{label}.{field}")
            evidence_sets[field] = set(evidence_ids)
            for experiment_id in evidence_ids:
                if experiment_id not in by_experiment:
                    raise RegistryError(f"{label}.{field} references unknown experiment {experiment_id}")
        overlap = evidence_sets["supported_by"] & evidence_sets["contradicted_by"]
        if overlap:
            raise RegistryError(f"{label} lists evidence as both support and contradiction: {sorted(overlap)}")
        next_preflight = item["next_preflight"]
        if next_preflight is not None:
            preflight = by_preflight.get(next_preflight)
            if preflight is None:
                raise RegistryError(f"{label}.next_preflight references unknown preflight {next_preflight}")
            if preflight["status"] != "pending":
                raise RegistryError(f"{label}.next_preflight must reference a pending preflight")
            if next_preflight in claimed_next_preflights:
                raise RegistryError(f"preflight {next_preflight} is claimed by more than one capability")
            claimed_next_preflights.add(next_preflight)
            if preflight["capability_id"] != capability_id:
                raise RegistryError(f"{label}.next_preflight must target its own capability")
        if item["status"] == "unresolved" and next_preflight is None:
            raise RegistryError(f"unresolved {label} must name one next_preflight")
        if item["status"] != "unresolved" and next_preflight is not None:
            raise RegistryError(f"resolved {label} cannot name a next_preflight")

    for preflight_id, preflight in by_preflight.items():
        if preflight["capability_id"] not in capability_ids:
            raise RegistryError(f"preflight {preflight_id} references unknown capability")
        if preflight["status"] == "pending" and preflight_id not in claimed_next_preflights:
            raise RegistryError(f"pending preflight {preflight_id} must be the next preflight for one capability")


def mutated_fixture(registry: dict[str, Any], mutation: str) -> Any:
    if mutation not in FIXTURE_MUTATIONS:
        raise RegistryError(f"unknown negative-fixture mutation {mutation!r}")
    if mutation == "duplicate-json-key":
        return load_json_text('{"schema_version": 1, "schema_version": 1}', mutation)
    candidate = copy.deepcopy(registry)
    if mutation == "duplicate-experiment-id":
        candidate["experiments"].append(candidate["experiments"][0])
    elif mutation == "missing-evidence":
        candidate["experiments"][0]["evidence"] = []
    elif mutation == "invalid-conflict-link":
        candidate["experiments"][0]["conflicts"].append("TB-EXP-9999-999")
    elif mutation == "empty-changed-variable":
        candidate["preflights"][0]["single_changed_variable"] = "  "
    elif mutation == "forbidden-privacy-key":
        candidate["experiments"][0]["prompt"] = "redacted"
    elif mutation == "forbidden-privacy-text":
        candidate["experiments"][0]["question"] = "/Users/example/private"
    elif mutation == "closed-preflight-missing-result":
        candidate["preflights"][0]["result_experiment_id"] = ""
    elif mutation == "closed-preflight-wrong-result":
        candidate["preflights"][0]["result_experiment_id"] = "TB-EXP-0102-001"
    elif mutation == "pending-preflight-with-result":
        candidate["preflights"][1]["status"] = "pending"
    elif mutation == "completed-next-preflight":
        candidate["capabilities"][1]["next_preflight"] = "TB-PRE-0108-001"
    elif mutation == "shared-next-preflight":
        candidate["preflights"][1]["status"] = "pending"
        candidate["preflights"][1]["result_experiment_id"] = ""
        for experiment in candidate["experiments"]:
            if experiment["preflight_id"] == candidate["preflights"][1]["id"]:
                experiment["preflight_id"] = None
        candidate["capabilities"][1]["status"] = "unresolved"
        candidate["capabilities"][1]["next_preflight"] = candidate["preflights"][1]["id"]
        duplicate = copy.deepcopy(candidate["capabilities"][1])
        duplicate["id"] = "TB-CAP-NATIVE-HOOK-PARTICIPATION-ALIAS"
        candidate["capabilities"].append(duplicate)
    elif mutation == "bare-unknown":
        candidate["experiments"][0]["result"]["timing_ms"] = "unknown"
    elif mutation == "capability-evidence-overlap":
        candidate["capabilities"][0]["contradicted_by"].append(
            candidate["capabilities"][0]["supported_by"][0]
        )
    return candidate


def run_negative_fixtures(registry: dict[str, Any], fixtures: Any) -> int:
    fixture_root = exact_object(fixtures, {"cases"}, "fixtures")
    cases = fixture_root["cases"]
    if not isinstance(cases, list) or not cases:
        raise RegistryError("fixtures.cases must contain at least one case")
    declared_mutations = {
        nonempty_string(case.get("mutation"), f"fixtures.cases[{index}].mutation")
        for index, case in enumerate(cases)
        if isinstance(case, dict)
    }
    if declared_mutations != FIXTURE_MUTATIONS:
        missing = sorted(FIXTURE_MUTATIONS - declared_mutations)
        extra = sorted(declared_mutations - FIXTURE_MUTATIONS)
        raise RegistryError(f"fixture mutation coverage mismatch; missing={missing}, extra={extra}")
    for index, raw in enumerate(cases):
        case = exact_object(raw, {"name", "mutation", "expected_error"}, f"fixtures.cases[{index}]")
        name = nonempty_string(case["name"], f"fixtures.cases[{index}].name")
        mutation = nonempty_string(case["mutation"], f"fixtures.cases[{index}].mutation")
        expected = nonempty_string(case["expected_error"], f"fixtures.cases[{index}].expected_error")
        try:
            candidate = mutated_fixture(registry, mutation)
            validate_registry(candidate)
        except RegistryError as error:
            if expected.lower() not in str(error).lower():
                raise RegistryError(f"negative fixture {name!r} failed for the wrong reason: {error}") from error
        else:
            raise RegistryError(f"negative fixture {name!r} unexpectedly passed")
    return len(cases)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--registry", type=pathlib.Path, default=DEFAULT_REGISTRY)
    parser.add_argument("--fixtures", type=pathlib.Path, default=DEFAULT_FIXTURES)
    args = parser.parse_args()
    try:
        registry = load_json(args.registry)
        validate_registry(registry)
        fixture_count = run_negative_fixtures(registry, load_json(args.fixtures))
    except RegistryError as error:
        print(f"experiment registry invalid: {error}", file=sys.stderr)
        return 1
    print(
        "experiment registry ok: "
        f"{len(registry['experiments'])} experiments, "
        f"{len(registry['capabilities'])} capabilities, "
        f"{len(registry['preflights'])} preflights; "
        f"{fixture_count} negative fixtures passed"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
