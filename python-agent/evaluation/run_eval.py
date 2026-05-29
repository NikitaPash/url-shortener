"""
Evaluation script for the AI Analytics Agent.
Scores Execution Accuracy (EX) by comparing agent results against reference SQL.

Usage:
    python evaluation/run_eval.py --user-id <uuid> --token <jwt>
"""

import argparse
import json

import clickhouse_connect
import httpx


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--user-id", required=True, help="User ID to inject into reference SQL")
    parser.add_argument("--base-url", default="http://localhost:8090")
    parser.add_argument("--token", required=True, help="JWT token for authentication")
    parser.add_argument("--benchmark", default="evaluation/benchmark.json")
    parser.add_argument("--clickhouse-host", default="localhost")
    parser.add_argument("--clickhouse-port", type=int, default=8123)
    args = parser.parse_args()

    with open(args.benchmark) as f:
        benchmark = json.load(f)

    ch = clickhouse_connect.get_client(
        host=args.clickhouse_host,
        port=args.clickhouse_port,
        database="shortener",
    )

    headers = {
        "Authorization": f"Bearer {args.token}",
        "Content-Type": "application/json",
    }

    passed = 0
    failed = 0
    errors = 0
    results_log = []

    for item in benchmark:
        qid = item["id"]
        question = item["question"]
        ref_sql = item["reference_sql"].replace("{user_id}", args.user_id) + " LIMIT 1000"

        print(f"\n--- Question {qid}: {question}")

        try:
            ref_result = ch.query(ref_sql)
            ref_data = sorted([list(row) for row in ref_result.result_rows])
        except Exception as e:
            print(f"  Reference SQL failed: {e}")
            errors += 1
            results_log.append({"id": qid, "status": "ref_error", "error": str(e)})
            continue

        try:
            resp = httpx.post(
                f"{args.base_url}/api/query",
                json={"question": question},
                headers=headers,
                timeout=30.0,
            )

            if resp.status_code != 200:
                print(f"  Agent returned {resp.status_code}: {resp.text}")
                errors += 1
                results_log.append({"id": qid, "status": "agent_error", "code": resp.status_code})
                continue

            agent_resp = resp.json()
            agent_data = sorted(agent_resp.get("data", []))

        except Exception as e:
            print(f"  Agent call failed: {e}")
            errors += 1
            results_log.append({"id": qid, "status": "agent_error", "error": str(e)})
            continue

        ref_normalized = sorted([[str(v) for v in row] for row in ref_data])
        agent_normalized = sorted([[str(v) for v in row] for row in agent_data])

        if ref_normalized == agent_normalized:
            print(f"  PASS (EX match)")
            passed += 1
            results_log.append({"id": qid, "status": "pass", "sql": agent_resp.get("sql", "")})
        else:
            print(f"  FAIL")
            print(f"    Agent SQL: {agent_resp.get('sql', 'N/A')}")
            print(f"    Ref rows: {len(ref_data)}, Agent rows: {len(agent_data)}")
            failed += 1
            results_log.append({
                "id": qid,
                "status": "fail",
                "agent_sql": agent_resp.get("sql", ""),
                "ref_rows": len(ref_data),
                "agent_rows": len(agent_data),
            })

    total = passed + failed + errors
    ex_accuracy = round(passed / max(total - errors, 1) * 100, 1)

    print(f"\n{'=' * 50}")
    print(f"RESULTS: {passed} passed, {failed} failed, {errors} errors out of {total}")
    print(f"Execution Accuracy (EX): {ex_accuracy}%")
    print(f"{'=' * 50}")

    with open("evaluation/results.json", "w") as f:
        json.dump({
            "summary": {"passed": passed, "failed": failed, "errors": errors, "ex_accuracy": ex_accuracy},
            "details": results_log,
        }, f, indent=2)
    print(f"\nDetailed results saved to evaluation/results.json")


if __name__ == "__main__":
    main()
