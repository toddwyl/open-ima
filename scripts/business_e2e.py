#!/usr/bin/env python3
"""Black-box V1 business workflow verification using only the public HTTP API."""

from __future__ import annotations

import argparse
import json
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path
from typing import Any


class E2EFailure(RuntimeError):
    pass


def pass_case(name: str) -> None:
    print(f"PASS {name}", flush=True)


def require(name: str, condition: bool, details: Any = None) -> None:
    if not condition:
        raise E2EFailure(f"{name}: {details!r}")
    pass_case(name)


class Client:
    def __init__(self, base_url: str):
        self.base_url = base_url.rstrip("/")

    def request(
        self,
        method: str,
        path: str,
        *,
        expected: int | tuple[int, ...] = 200,
        json_body: Any = None,
        body: bytes | None = None,
        headers: dict[str, str] | None = None,
    ) -> tuple[int, bytes]:
        request_headers = dict(headers or {})
        if json_body is not None:
            body = json.dumps(json_body).encode()
            request_headers["Content-Type"] = "application/json"
        request = urllib.request.Request(
            self.base_url + path, data=body, headers=request_headers, method=method
        )
        try:
            with urllib.request.urlopen(request, timeout=15) as response:
                status, payload = response.status, response.read()
        except urllib.error.HTTPError as error:
            status, payload = error.code, error.read()
        expected_statuses = (expected,) if isinstance(expected, int) else expected
        if status not in expected_statuses:
            raise E2EFailure(
                f"{method} {path}: status {status}, expected {expected_statuses}, body={payload.decode(errors='replace')}"
            )
        return status, payload

    def json(self, method: str, path: str, **kwargs: Any) -> Any:
        _, payload = self.request(method, path, **kwargs)
        return json.loads(payload or b"null")

    def upload(
        self, kb_id: str, filename: str, content: bytes, *, expected: int = 202
    ) -> Any:
        boundary = "open-ima-e2e-" + uuid.uuid4().hex
        body = (
            f"--{boundary}\r\n"
            f'Content-Disposition: form-data; name="file"; filename="{filename}"\r\n'
            "Content-Type: application/octet-stream\r\n\r\n"
        ).encode() + content + f"\r\n--{boundary}--\r\n".encode()
        return self.json(
            "POST",
            f"/api/kbs/{kb_id}/medias",
            expected=expected,
            body=body,
            headers={"Content-Type": f"multipart/form-data; boundary={boundary}"},
        )


def parse_sse(payload: bytes) -> list[tuple[str, Any]]:
    events: list[tuple[str, Any]] = []
    event_name = "message"
    data_lines: list[str] = []
    for line in payload.decode().splitlines() + [""]:
        if line.startswith("event:"):
            event_name = line.removeprefix("event:").strip()
        elif line.startswith("data:"):
            data_lines.append(line.removeprefix("data:").strip())
        elif line == "" and data_lines:
            events.append((event_name, json.loads("\n".join(data_lines))))
            event_name, data_lines = "message", []
    return events


def find_media(client: Client, kb_id: str, media_id: str) -> dict[str, Any] | None:
    medias = client.json("GET", f"/api/kbs/{kb_id}/medias")
    return next((item for item in medias if item["biz_id"] == media_id), None)


def wait_media(
    client: Client, kb_id: str, media_id: str, expected_status: str, timeout: float = 20
) -> dict[str, Any]:
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        last = find_media(client, kb_id, media_id)
        if last and last["status"] == expected_status:
            return last
        time.sleep(0.2)
    raise E2EFailure(
        f"media {media_id} did not become {expected_status}; last={last!r}"
    )


def wait_media_absent(client: Client, kb_id: str, media_id: str) -> None:
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        if media(client, kb_id, media_id) is None:
            return
        time.sleep(0.2)
    raise E2EFailure(f"media {media_id} was not deleted")


def wait_search_absent(client: Client, kb_id: str, media_ids: set[str]) -> None:
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        query = urllib.parse.urlencode({"q": "Open IMA", "mode": "text"})
        results = client.json("GET", f"/api/kbs/{kb_id}/search?{query}")
        if media_ids.isdisjoint({item["media_biz_id"] for item in results}):
            return
        time.sleep(0.2)
    raise E2EFailure(f"medias remain searchable: {media_ids}")


def wait_meili_absent(meili: Client, kb_id: str, media_ids: set[str]) -> None:
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        results = meili.json(
            "POST",
            "/indexes/chunks/search",
            json_body={"filter": f"kb_biz_id = '{kb_id}'", "limit": 100},
        )
        if media_ids.isdisjoint({item["media_biz_id"] for item in results["hits"]}):
            return
        time.sleep(0.2)
    raise E2EFailure(f"medias remain in Meilisearch: {media_ids}")


def run(base_url: str, meili_url: str, fixture_dir: Path, fixture_url: str) -> None:
    client = Client(base_url)
    meili = Client(meili_url)
    markdown = (fixture_dir / "smoke.md").read_bytes()
    pdf = (fixture_dir / "smoke.pdf").read_bytes()
    blank_pdf = (fixture_dir / "blank.pdf").read_bytes()

    health = client.json("GET", "/health")
    require("health endpoint", health == {"status": "ok"}, health)
    _, index = client.request("GET", "/workspace/deep-link")
    require("embedded SPA fallback", b'<div id="root"></div>' in index)

    client.request("POST", "/api/kbs", expected=400, json_body={"name": " "})
    pass_case("knowledge-base name validation")
    kb = client.json(
        "POST",
        "/api/kbs",
        expected=201,
        json_body={"name": "Business E2E", "description": "complete matrix"},
    )
    other_kb = client.json(
        "POST", "/api/kbs", expected=201, json_body={"name": "Other KB"}
    )
    client.request(
        "POST", "/api/kbs", expected=409, json_body={"name": "Business E2E"}
    )
    listed = client.json("GET", "/api/kbs")
    require(
        "knowledge-base create/list/duplicate",
        {item["biz_id"] for item in listed} == {kb["biz_id"], other_kb["biz_id"]}
        and all(item["doc_count"] == 0 for item in listed),
        listed,
    )

    client.request(
        "POST",
        f"/api/kbs/{kb['biz_id']}/medias",
        expected=400,
        body=b"not multipart",
        headers={"Content-Type": "text/plain"},
    )
    client.upload(kb["biz_id"], "unsupported.exe", b"no", expected=400)
    client.upload("missing-kb", "orphan.md", markdown, expected=404)
    pass_case("upload validation and unknown KB rejection")

    markdown_upload = client.upload(kb["biz_id"], "smoke.md", markdown)
    duplicate_upload = client.upload(kb["biz_id"], "same-content.md", markdown)
    require(
        "file hash deduplication",
        markdown_upload["duplicate"] is False
        and duplicate_upload == {
            "media_biz_id": markdown_upload["media_biz_id"],
            "duplicate": True,
        },
        duplicate_upload,
    )
    pdf_upload = client.upload(kb["biz_id"], "smoke.pdf", pdf)
    failed_upload = client.upload(kb["biz_id"], "blank.pdf", blank_pdf)
    markdown_doc = wait_media(client, kb["biz_id"], markdown_upload["media_biz_id"], "ready")
    pdf_doc = wait_media(client, kb["biz_id"], pdf_upload["media_biz_id"], "ready")
    failed_doc = wait_media(client, kb["biz_id"], failed_upload["media_biz_id"], "failed")
    require(
        "Markdown and PDF parsing",
        markdown_doc["chunk_count"] > 0 and pdf_doc["chunk_count"] > 0,
        [markdown_doc, pdf_doc],
    )
    require("parser failure is visible", bool(failed_doc["error"]), failed_doc)

    client.request(
        "POST", f"/api/medias/{markdown_doc['biz_id']}/retry", expected=409
    )
    client.request("POST", "/api/medias/missing-media/retry", expected=404)
    client.request("POST", f"/api/medias/{failed_doc['biz_id']}/retry", expected=202)
    failed_again = wait_media(client, kb["biz_id"], failed_doc["biz_id"], "failed")
    require("failed media manual retry", bool(failed_again["error"]), failed_again)

    client.request(
        "POST",
        f"/api/kbs/{kb['biz_id']}/medias:url",
        expected=400,
        json_body={"url": "file:///etc/passwd"},
    )
    client.request(
        "POST",
        "/api/kbs/missing-kb/medias:url",
        expected=404,
        json_body={"url": fixture_url + "/page.html"},
    )
    url_upload = client.json(
        "POST",
        f"/api/kbs/{kb['biz_id']}/medias:url",
        expected=202,
        json_body={"url": fixture_url + "/page.html"},
    )
    duplicate_url = client.json(
        "POST",
        f"/api/kbs/{kb['biz_id']}/medias:url",
        expected=202,
        json_body={"url": fixture_url + "/page.html"},
    )
    require(
        "URL content deduplication",
        duplicate_url["duplicate"] is True
        and duplicate_url["media_biz_id"] == url_upload["media_biz_id"],
        duplicate_url,
    )
    url_doc = wait_media(client, kb["biz_id"], url_upload["media_biz_id"], "ready")
    require("URL ingestion and parsing", url_doc["source_type"] == "url", url_doc)

    listed = client.json("GET", "/api/kbs")
    listed_kb = next(item for item in listed if item["biz_id"] == kb["biz_id"])
    require("knowledge-base media count", listed_kb["doc_count"] == 4, listed_kb)

    ready_ids = {markdown_doc["biz_id"], pdf_doc["biz_id"], url_doc["biz_id"]}
    for mode in ("hybrid", "text"):
        query = urllib.parse.urlencode({"q": "Open IMA", "mode": mode})
        results = client.json("GET", f"/api/kbs/{kb['biz_id']}/search?{query}")
        require(
            f"{mode} search",
            bool(results)
            and {item["media_biz_id"] for item in results}.issubset(ready_ids)
            and all(item["chunk_biz_id"] and item["snippet"] for item in results),
            results,
        )
    client.request("GET", f"/api/kbs/{kb['biz_id']}/search?q=&mode=text", expected=400)
    client.request(
        "GET", f"/api/kbs/{kb['biz_id']}/search?q=x&mode=invalid", expected=400
    )
    client.request("GET", "/api/kbs/missing-kb/search?q=x&mode=text", expected=404)
    pass_case("search validation and unknown KB rejection")

    client.request(
        "POST", f"/api/kbs/{kb['biz_id']}/chat", expected=400, json_body={"query": " "}
    )
    _, chat_payload = client.request(
        "POST",
        f"/api/kbs/{kb['biz_id']}/chat",
        json_body={"query": "What is the Project Atlas launch code?"},
    )
    events = parse_sse(chat_payload)
    event_names = [name for name, _ in events]
    done = next(data for name, data in events if name == "done")
    citations = next(data for name, data in events if name == "citations")
    answer = "".join(data["token"] for name, data in events if name == "token")
    require(
        "PDF-grounded streaming chat with citation",
        "token" in event_names
        and "ORCHID-7429" in answer
        and any(item["media_biz_id"] == pdf_doc["biz_id"] for item in citations)
        and done["conversation_biz_id"],
        events,
    )
    conversation_id = done["conversation_biz_id"]
    _, continued_payload = client.request(
        "POST",
        f"/api/kbs/{kb['biz_id']}/chat",
        json_body={"conversation_biz_id": conversation_id, "query": "Continue that answer"},
    )
    continued_events = parse_sse(continued_payload)
    require(
        "continued streaming chat",
        any(name == "done" and data["conversation_biz_id"] == conversation_id for name, data in continued_events),
        continued_events,
    )
    conversations = client.json("GET", f"/api/kbs/{kb['biz_id']}/conversations")
    messages = client.json("GET", f"/api/conversations/{conversation_id}/messages")
    require(
        "conversation and four-message history",
        len(conversations) == 1
        and conversations[0]["biz_id"] == conversation_id
        and [item["role"] for item in messages] == ["user", "assistant", "user", "assistant"]
        and all(item["citations"] for item in messages if item["role"] == "assistant"),
        {"conversations": conversations, "messages": messages},
    )
    _, cross_kb_payload = client.request(
        "POST",
        f"/api/kbs/{other_kb['biz_id']}/chat",
        json_body={"conversation_biz_id": conversation_id, "query": "wrong owner"},
    )
    cross_events = parse_sse(cross_kb_payload)
    require(
        "cross-KB conversation rejection",
        [name for name, _ in cross_events] == ["error"],
        cross_events,
    )

    client.request(
        "GET", f"/internal/files/{markdown_doc['source_uri']}", expected=403
    )
    pass_case("internal file authorization")

    client.request("DELETE", f"/api/medias/{markdown_doc['biz_id']}", expected=204)
    wait_media_absent(client, kb["biz_id"], markdown_doc["biz_id"])
    wait_search_absent(client, kb["biz_id"], {markdown_doc["biz_id"]})
    client.request("DELETE", "/api/medias/missing-media", expected=404)
    pass_case("media deletion and search cleanup")

    remaining_ids = {pdf_doc["biz_id"], failed_doc["biz_id"], url_doc["biz_id"]}
    client.request("DELETE", f"/api/kbs/{kb['biz_id']}", expected=204)
    listed = client.json("GET", "/api/kbs")
    require(
        "knowledge-base deletion",
        {item["biz_id"] for item in listed} == {other_kb["biz_id"]},
        listed,
    )
    client.request("GET", f"/api/kbs/{kb['biz_id']}/search?q=x&mode=text", expected=404)
    wait_meili_absent(meili, kb["biz_id"], remaining_ids)
    require(
        "conversation cascade deletion",
        client.json("GET", f"/api/conversations/{conversation_id}/messages") == [],
    )
    client.request("DELETE", "/api/kbs/missing-kb", expected=404)
    pass_case("unknown knowledge-base deletion rejection")

    client.request("DELETE", f"/api/kbs/{other_kb['biz_id']}", expected=204)
    require("final empty knowledge-base list", client.json("GET", "/api/kbs") == [])


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--meili-url", required=True)
    parser.add_argument("--fixture-dir", type=Path, required=True)
    parser.add_argument("--fixture-url", required=True)
    args = parser.parse_args()
    run(args.base_url, args.meili_url, args.fixture_dir, args.fixture_url.rstrip("/"))
    print("BUSINESS E2E OK", flush=True)


if __name__ == "__main__":
    main()
