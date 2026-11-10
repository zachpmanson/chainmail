#!/usr/bin/env python3
"""Offline report comparison, not a builder, provider probe, or parity claim."""

import argparse
import copy
import datetime as dt
import hashlib
import json
import pathlib
import sys
import tempfile
import unittest

CORPUS_SHA256 = "f066f7a4da030781aecde87b42a467bef58df2a13f42c3232d1cd44a46c5a62e"
WEB_CORPUS = "internal/hassles/testdata/format-contract-v1.json"
ANDROID_CORPUS = "android/app/src/test/resources/format-contract-v1.json"
NATIVE_PIN = "android/app/src/test/java/com/zachmanson/hassles/calendar/FormatContractFixture.kt"
AUTHORITY = {"web": "web-evaluation", "android": "native-instances"}


class ContractError(ValueError):
    pass


def require(ok, message):
    if not ok:
        raise ContractError(message)


def fields(value, required, optional=()):
    require(isinstance(value, dict), "expected object")
    require(set(required) <= value.keys(), f"missing fields: {set(required) - value.keys()}")
    require(value.keys() <= set(required) | set(optional), f"unknown fields: {value.keys() - set(required) - set(optional)}")


def unique_object(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, f"duplicate JSON field {key}")
        value[key] = item
    return value


def decode(raw):
    try:
        return json.loads(raw, object_pairs_hook=unique_object,
                          parse_constant=lambda x: (_ for _ in ()).throw(ContractError(f"nonfinite {x}")))
    except (ValueError, UnicodeError) as error:
        raise ContractError(f"invalid JSON: {error}") from error


def leaves(value, path=""):
    if isinstance(value, dict) and value:
        result = {}
        for key, item in value.items():
            key = key.replace("~", "~0").replace("/", "~1")
            result.update(leaves(item, path + "/" + key))
        return result
    return {path: value}


def equal(left, right):
    # bool is an int subclass in Python; JSON true is NOT JSON 1.
    if isinstance(left, bool) or isinstance(right, bool):
        return type(left) is type(right) and left == right
    return left == right


def typed_date(value):
    fields(value, ("type", "raw"), ("tzid",))
    require(value["type"] in ("DATE", "UTC", "LOCAL"), "unknown typed date")
    require((value["type"] == "LOCAL") == ("tzid" in value), "typed date TZID mismatch")
    require(isinstance(value["raw"], str), "raw date not text")


def observation_shape(value, kind):
    require(isinstance(value, dict), "observation is not an object")
    fields(value.get("validation"), ("accepted",))
    if kind == "schedule":
        fields(value, ("validation", "emission", "actions"))
        require(type(value["validation"].get("accepted")) is bool, "missing schedule validation")
        if value["emission"] is not None:
            fields(value["emission"], ("start", "timeZone", "durationSeconds", "cadence", "interval", "until", "workHours", "rule"))
        names = {"create"}
    else:
        fields(value, ("validation", "profile", "capabilities", "timing", "actions"))
        require(value["validation"].get("accepted", "absent") is None, "import validation must be null")
        fields(value["profile"], ("recognized", "cadence", "interval", "until", "workHours"))
        fields(value["capabilities"], ("edit", "titleOnly", "instance", "allDelete"))
        fields(value["timing"], ("knowledge", "next", "last", "allDay"))
        names = {"edit", "titleOnly", "instance", "all"}
    fields(value["validation"], ("accepted",))
    require(set(value["actions"]) == names, "action coverage mismatch")
    for action in value["actions"].values():
        fields(action, ("accepted", "writes", "effect", "target", "witness", "preserved"))
        require(type(action["accepted"]) is bool and type(action["preserved"]) is bool, "action flags not booleans")
        require(type(action["writes"]) is int and action["writes"] >= 0, "invalid write count")
        require(action["effect"] in ("none", "create", "update", "exclude", "delete"), "unknown action effect")


def corpus_schema(corpus):
    fields(corpus, ("schemaVersion", "contractId", "cases"))
    require(type(corpus["schemaVersion"]) is int and corpus["schemaVersion"] == 1, "unsupported schemaVersion")
    require(corpus["contractId"] == "hassles-format-v1", "unsupported contractId")
    require(isinstance(corpus["cases"], list) and len(corpus["cases"]) == 185, "case count changed")
    seen = set()
    counts = {"schedule": 0, "import": 0}
    for case in corpus["cases"]:
        fields(case, ("id", "kind", "now", "nativeInstances", "actions", "expected", "differences"), ("schedule", "events"))
        require(isinstance(case["id"], str) and case["id"] and case["id"] not in seen, "duplicate/empty case ID")
        seen.add(case["id"])
        kind = case["kind"]
        require(kind in counts, "unknown kind")
        counts[kind] += 1
        if kind == "schedule":
            require("events" not in case and "schedule" in case, "invalid schedule input")
            fields(case["schedule"], ("title", "start", "timeZone", "cadence", "interval", "until", "workHours"))
            require(case["actions"] == ["create"], "schedule action coverage")
            require(case["nativeInstances"] == [], "unexpected schedule Instances")
        else:
            require("schedule" not in case and case.get("events"), "invalid import input")
            require(case["actions"] == ["edit", "titleOnly", "instance", "all"], "import action coverage")
            keys = set()
            for event in case["events"]:
                fields(event, ("key", "uid", "description", "title", "dtstart", "durationSeconds", "alarmMinutes", "attendees", "etagStrength"), ("dtend", "rrule", "rdate", "exdate", "organizer", "detachedFrom"))
                require(event["key"] not in keys and event["key"], "duplicate/empty event key")
                keys.add(event["key"])
                typed_date(event["dtstart"])
                if "dtend" in event:
                    typed_date(event["dtend"])
                for name in ("rdate", "exdate"):
                    for date in event.get(name, []):
                        typed_date(date)
                require(event["etagStrength"] in ("strong", "weak"), "unknown ETag strength")
            for event in case["events"]:
                if "detachedFrom" in event:
                    require(event["detachedFrom"] in keys, "unknown detached master")
            for instance in case["nativeInstances"]:
                fields(instance, ("eventKey", "begin", "end"))
                require(instance["eventKey"] in keys, "unknown instance key")
        observation_shape(case["expected"], kind)
        baseline = leaves(case["expected"])
        paths = set()
        for difference in case["differences"]:
            fields(difference, ("path", "web", "android", "reason", "source"))
            path = difference["path"]
            require(path in baseline and path not in paths, f"unused/duplicate difference {case['id']}{path}")
            require(equal(baseline[path], difference["web"]), f"baseline differs from web delta {case['id']}{path}")
            require(not equal(difference["web"], difference["android"]), f"stale/converged difference {case['id']}{path}")
            require(isinstance(difference["reason"], str) and difference["reason"].strip(), "missing difference reason")
            require(isinstance(difference["source"], list) and len(difference["source"]) >= 2 and all(isinstance(s, str) and ":" in s for s in difference["source"]), "missing difference sources")
            paths.add(path)
    require(counts == {"schedule": 137, "import": 48}, "kind coverage changed")


def report_schema(report, platform, corpus, corpus_mtime):
    fields(report, ("schemaVersion", "contractId", "corpusSha256", "platform", "timingAuthority", "generatedAt", "passed", "consumedCaseCount", "cases"))
    require(type(report["schemaVersion"]) is int and report["schemaVersion"] == 1 and report["contractId"] == corpus["contractId"], "report version mismatch")
    require(report["corpusSha256"] == CORPUS_SHA256, "stale report hash")
    require(report["platform"] == platform and report["timingAuthority"] == AUTHORITY[platform], "platform/authority metadata mismatch")
    require(report["passed"] is True, f"{platform} runner reports failure")
    require(type(report["consumedCaseCount"]) is int and report["consumedCaseCount"] == len(corpus["cases"]), "unconsumed cases")
    require(isinstance(report["generatedAt"], str), "report timestamp is not text")
    try:
        instant = dt.datetime.fromisoformat(report["generatedAt"].replace("Z", "+00:00"))
        require(instant.tzinfo is not None and instant.utcoffset() == dt.timedelta(0), "report timestamp must be UTC")
        generated = instant.timestamp()
    except (TypeError, ValueError) as error:
        raise ContractError("invalid report timestamp") from error
    age = dt.datetime.now(dt.timezone.utc).timestamp() - generated
    require(-5 <= age <= 3600 and generated >= corpus_mtime, "stale/future report; regenerate after local corpus copy")
    cases = {}
    for case in report["cases"]:
        fields(case, ("id", "passed", "observation"))
        require(case["id"] not in cases, "duplicate report case")
        require(case["passed"] is True, f"failed report case {case['id']}")
        cases[case["id"]] = case["observation"]
    require(cases.keys() == {case["id"] for case in corpus["cases"]}, "missing/new report cases")
    return cases


def compare(web, android, web_report, android_report):
    web = pathlib.Path(web)
    android = pathlib.Path(android)
    web_path, android_path = web / WEB_CORPUS, android / ANDROID_CORPUS
    web_bytes, android_bytes = web_path.read_bytes(), android_path.read_bytes()
    require(web_bytes == android_bytes, "vendor corpus bytes differ")
    require(hashlib.sha256(web_bytes).hexdigest() == CORPUS_SHA256, "corpus SHA pin mismatch")
    require(f'const formatCorpusSHA = "{CORPUS_SHA256}"' in (web / "internal/hassles/format_conformance_test.go").read_text(), "web test hash pin missing")
    require(f'internal const val CONTRACT_SHA = "{CORPUS_SHA256}"' in (android / NATIVE_PIN).read_text(), "Android test hash pin missing")
    corpus = decode(web_bytes)
    corpus_schema(corpus)
    reports = {
        "web": report_schema(decode(pathlib.Path(web_report).read_bytes()), "web", corpus, web_path.stat().st_mtime),
        "android": report_schema(decode(pathlib.Path(android_report).read_bytes()), "android", corpus, android_path.stat().st_mtime),
    }
    common = 0
    known = []
    for case in corpus["cases"]:
        baseline = leaves(case["expected"])
        differences = {item["path"]: item for item in case["differences"]}
        actual = {}
        for platform in reports:
            observation = reports[platform][case["id"]]
            observation_shape(observation, case["kind"])
            actual[platform] = leaves(observation)
            require(actual[platform].keys() == baseline.keys(), f"{platform} leaf coverage mismatch {case['id']}")
            for path, expected in baseline.items():
                if path in differences:
                    expected = differences[path][platform]
                require(equal(actual[platform][path], expected), f"{platform} unexplained observation {case['id']}{path}: {actual[platform][path]!r} != {expected!r}")
        for path in baseline:
            if path in differences:
                require(not equal(actual["web"][path], actual["android"][path]), f"converged difference {case['id']}{path}")
                item = differences[path]
                known.append(f"{case['id']}{path}: {json.dumps(item['web'])} => {json.dumps(item['android'])} — {item['reason']} [{'; '.join(item['source'])}]")
            else:
                require(equal(actual["web"][path], actual["android"][path]), f"undeclared difference {case['id']}{path}")
                common += 1
    return common, known


class ComparatorNegatives(unittest.TestCase):
    """Fabricated reports test comparator rejection only; NEVER integration evidence."""

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.web, self.android = self.root / "web", self.root / "android"
        self.raw = (pathlib.Path(__file__).resolve().parents[1] / WEB_CORPUS).read_bytes()
        self.corpus = decode(self.raw)
        self.write(self.web / WEB_CORPUS, self.raw)
        self.write(self.android / ANDROID_CORPUS, self.raw)
        self.write(self.web / "internal/hassles/format_conformance_test.go", f'const formatCorpusSHA = "{CORPUS_SHA256}"'.encode())
        self.write(self.android / NATIVE_PIN, f'internal const val CONTRACT_SHA = "{CORPUS_SHA256}"'.encode())
        self.reports = {}
        for platform in AUTHORITY:
            cases = []
            for case in self.corpus["cases"]:
                observation = copy.deepcopy(case["expected"])
                if platform == "android":
                    for difference in case["differences"]:
                        keys = difference["path"].split('/')[1:]
                        target = observation
                        for key in keys[:-1]:
                            target = target[key]
                        target[keys[-1]] = difference["android"]
                cases.append(dict(id=case["id"], passed=True, observation=observation))
            self.reports[platform] = dict(schemaVersion=1, contractId="hassles-format-v1", corpusSha256=CORPUS_SHA256, platform=platform, timingAuthority=AUTHORITY[platform], generatedAt=dt.datetime.now(dt.timezone.utc).isoformat(), passed=True, consumedCaseCount=len(cases), cases=cases)
        self.save_reports()

    def write(self, path, raw):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(raw)

    def save_reports(self):
        for platform, value in self.reports.items():
            self.write(self.root / (platform + '.json'), json.dumps(value).encode())

    def compare(self):
        return compare(self.web, self.android, self.root / 'web.json', self.root / 'android.json')

    def rejected(self):
        with self.assertRaises((ContractError, OSError)):
            self.compare()

    def test_control(self):
        common, known = self.compare()
        self.assertGreater(common, 2000)
        self.assertEqual(len(known), 335)

    def test_missing_android_pin(self):
        (self.android / NATIVE_PIN).unlink()
        self.rejected()

    def test_wrong_android_pin(self):
        self.write(self.android / NATIVE_PIN, f'internal const val CONTRACT_SHA = "{"0" * 64}"'.encode())
        self.rejected()

    def test_hash_in_comment_is_not_web_pin(self):
        self.write(self.web / "internal/hassles/format_conformance_test.go", f'// {CORPUS_SHA256}\nconst formatCorpusSHA = "{"0" * 64}"'.encode())
        self.rejected()

    def test_corrupt_vendor_byte(self):
        self.write(self.android / ANDROID_CORPUS, self.raw + b' ')
        self.rejected()

    def test_both_corpora_hash_corruption(self):
        for root, path in [(self.web, WEB_CORPUS), (self.android, ANDROID_CORPUS)]:
            self.write(root / path, self.raw + b' ')
        self.rejected()

    def test_altered_observation(self):
        self.reports['web']['cases'][0]['observation']['emission']['durationSeconds'] = 301
        self.save_reports()
        self.rejected()

    def test_undeclared_difference(self):
        self.reports['android']['cases'][0]['observation']['actions']['create']['target'] = 'retargeted'
        self.save_reports()
        self.rejected()

    def test_converged_difference(self):
        for case in self.corpus['cases']:
            if case['differences']:
                item = case['differences'][0]
                report_case = next(c for c in self.reports['android']['cases'] if c['id'] == case['id'])
                keys = item['path'].split('/')[1:]
                target = report_case['observation']
                for key in keys[:-1]:
                    target = target[key]
                target[keys[-1]] = item['web']
                break
        self.save_reports()
        self.rejected()

    def test_unused_difference(self):
        self.corpus['cases'][0]['differences'] = [dict(path='/missing', web=True, android=False, reason='test', source=['web:f', 'android:f'])]
        with self.assertRaises(ContractError):
            corpus_schema(self.corpus)

    def test_missing_case(self):
        self.reports['android']['cases'].pop()
        self.save_reports()
        self.rejected()

    def test_missing_report(self):
        (self.root / 'android.json').unlink()
        self.rejected()

    def test_stale_report(self):
        self.reports['android']['corpusSha256'] = '0' * 64
        self.save_reports()
        self.rejected()

    def test_old_report(self):
        self.reports['android']['generatedAt'] = '2026-01-01T00:00:00Z'
        self.save_reports()
        self.rejected()

    def test_new_leaf(self):
        self.reports['web']['cases'][0]['observation']['actions']['create']['unexplained'] = False
        self.save_reports()
        self.rejected()

    def test_failed_runner(self):
        self.reports['web']['passed'] = False
        self.save_reports()
        self.rejected()

    def test_strict_unknown_duplicate_id_version(self):
        for change in ('unknown', 'duplicate', 'version'):
            value = copy.deepcopy(self.corpus)
            if change == 'unknown':
                value['cases'][0]['unexpected'] = True
            elif change == 'duplicate':
                value['cases'][1]['id'] = value['cases'][0]['id']
            else:
                value['schemaVersion'] = 2
            with self.assertRaises(ContractError):
                corpus_schema(value)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--web', type=pathlib.Path)
    parser.add_argument('--android', type=pathlib.Path)
    parser.add_argument('--web-report', type=pathlib.Path)
    parser.add_argument('--android-report', type=pathlib.Path)
    parser.add_argument('--self-test', action='store_true')
    args = parser.parse_args()
    if args.self_test:
        result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ComparatorNegatives))
        return 0 if result.wasSuccessful() else 1
    if not all((args.web, args.android, args.web_report, args.android_report)):
        parser.error('explicit --web, --android, --web-report and --android-report required')
    try:
        common, known = compare(args.web, args.android, args.web_report, args.android_report)
    except (ContractError, OSError, TypeError, KeyError) as error:
        print(f'FAIL: {error}', file=sys.stderr)
        return 1
    for line in known:
        print(line)
    print(f'{common} common checks; {len(known)} known differing paths')
    if known:
        print(f'PASS: shared contract; NOT full parity ({len(known)} known differing paths)')
    else:
        print('PASS: shared contract (declared semantics only; NOT provider-expansion proof)')
    return 0


if __name__ == '__main__':
    sys.exit(main())
