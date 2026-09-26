import httpx

from app.errors import FetchFailure

_MAX_BYTES = 50 * 1024 * 1024  # 50MB


async def fetch_file(url: str) -> bytes:
    try:
        async with httpx.AsyncClient(timeout=30.0, follow_redirects=True) as client:
            async with client.stream("GET", url) as resp:
                if resp.status_code != 200:
                    raise FetchFailure(f"fetch {url}: status {resp.status_code}")
                chunks: list[bytes] = []
                size = 0
                async for chunk in resp.aiter_bytes(65536):
                    size += len(chunk)
                    if size > _MAX_BYTES:
                        raise FetchFailure(f"fetch {url}: file exceeds 50MB limit")
                    chunks.append(chunk)
                return b"".join(chunks)
    except httpx.HTTPError as exc:
        raise FetchFailure(f"fetch {url}: {exc}") from exc
