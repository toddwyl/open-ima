import pytest
from fastapi.testclient import TestClient

from app import main
from app.errors import FetchFailure

client = TestClient(main.app)


def test_health():
    assert client.get("/health").json() == {"status": "ok"}


def test_parse_txt_via_api(sample_txt, monkeypatch):
    async def fake_fetch(url: str) -> bytes:
        return sample_txt

    monkeypatch.setattr(main, "fetch_file", fake_fetch)
    resp = client.post("/parse", json={"file_url": "http://x/f.txt", "file_type": "txt"})
    assert resp.status_code == 200
    body = resp.json()
    assert body["title"] == "f"
    assert body["blocks"][0]["type"] == "paragraph"


def test_parse_unsupported_type(monkeypatch):
    async def fake_fetch(url: str) -> bytes:
        return b"x"

    monkeypatch.setattr(main, "fetch_file", fake_fetch)
    resp = client.post("/parse", json={"file_url": "http://x/f.exe", "file_type": "exe"})
    assert resp.status_code == 422
    assert "error" in resp.json()


def test_parse_fetch_failure_returns_502(monkeypatch):
    async def fake_fetch(url: str) -> bytes:
        raise FetchFailure("connection refused")

    monkeypatch.setattr(main, "fetch_file", fake_fetch)
    resp = client.post("/parse", json={"file_url": "http://x/f.txt", "file_type": "txt"})
    assert resp.status_code == 502
    assert "connection refused" in resp.json()["error"]
