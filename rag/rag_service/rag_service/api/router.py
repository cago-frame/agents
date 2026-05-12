"""Top-level API router aggregating all versioned routes."""

from __future__ import annotations

from fastapi import APIRouter

from rag_service.api.v1.documents import router as documents_router
from rag_service.api.v1.embeddings import router as embeddings_router
from rag_service.api.v1.indices import router as indices_router
from rag_service.api.v1.search import router as search_router

api_router = APIRouter(prefix="/api/v1")

api_router.include_router(embeddings_router, tags=["embeddings"])
api_router.include_router(documents_router, tags=["documents"])
api_router.include_router(search_router, tags=["search"])
api_router.include_router(indices_router, tags=["indices"])
