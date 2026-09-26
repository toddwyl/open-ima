from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from app.errors import FetchFailure, ParseFailure
from app.fetcher import fetch_file
from app.models import ParseRequest, ParseResponse
from app.registry import get_parser

app = FastAPI(title="open-ima parser")


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok"}


@app.exception_handler(ParseFailure)
def parse_failure_handler(_: Request, exc: ParseFailure) -> JSONResponse:
    return JSONResponse(status_code=422, content={"error": str(exc)})


@app.exception_handler(FetchFailure)
def fetch_failure_handler(_: Request, exc: FetchFailure) -> JSONResponse:
    return JSONResponse(status_code=502, content={"error": str(exc)})


@app.post("/parse", response_model=ParseResponse)
async def parse(req: ParseRequest) -> ParseResponse:
    parser = get_parser(req.file_type)
    data = await fetch_file(req.file_url)
    filename = req.file_url.rsplit("/", 1)[-1] or f"file.{req.file_type}"
    title, blocks = parser.parse(data, filename)
    return ParseResponse(title=title, blocks=blocks)
