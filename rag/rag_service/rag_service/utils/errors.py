"""Custom exceptions and global error handlers for the RAG service."""

from __future__ import annotations

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse


class RAGServiceError(Exception):
    """Base exception for RAG service."""

    def __init__(self, message: str, status_code: int = 500):
        self.message = message
        self.status_code = status_code
        super().__init__(message)


class EmbeddingError(RAGServiceError):
    """Error during embedding generation."""

    def __init__(self, message: str):
        super().__init__(message, status_code=502)


class DocumentParseError(RAGServiceError):
    """Error during document parsing."""

    def __init__(self, message: str):
        super().__init__(message, status_code=422)


class VectorStoreError(RAGServiceError):
    """Error during vector store operations."""

    def __init__(self, message: str):
        super().__init__(message, status_code=502)


class IndexNotFoundError(RAGServiceError):
    """Requested index does not exist."""

    def __init__(self, index_name: str):
        super().__init__(f"Index '{index_name}' not found", status_code=404)


class UnsupportedFileTypeError(RAGServiceError):
    """Uploaded file type is not supported."""

    def __init__(self, mime_type: str):
        super().__init__(f"Unsupported file type: {mime_type}", status_code=415)


class ProviderNotConfiguredError(RAGServiceError):
    """Required provider is not properly configured."""

    def __init__(self, provider: str, detail: str = ""):
        msg = f"Provider '{provider}' is not properly configured"
        if detail:
            msg += f": {detail}"
        super().__init__(msg, status_code=503)


def register_error_handlers(app: FastAPI) -> None:
    """Register global error handlers on the FastAPI application."""

    @app.exception_handler(RAGServiceError)
    async def rag_service_error_handler(_request: Request, exc: RAGServiceError) -> JSONResponse:
        return JSONResponse(
            status_code=exc.status_code,
            content={"error": type(exc).__name__, "message": exc.message},
        )

    @app.exception_handler(Exception)
    async def unhandled_error_handler(_request: Request, exc: Exception) -> JSONResponse:
        return JSONResponse(
            status_code=500,
            content={"error": "InternalServerError", "message": str(exc)},
        )
