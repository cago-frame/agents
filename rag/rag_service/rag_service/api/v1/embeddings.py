"""Embeddings API endpoint: POST /api/v1/embeddings."""

from __future__ import annotations

from fastapi import APIRouter, Depends

from rag_service.api.v1.schemas.embeddings import (
    EmbeddingData,
    EmbeddingRequest,
    EmbeddingResponse,
)
from rag_service.dependencies import get_embedding_provider
from rag_service.embedding.base import EmbeddingProvider

router = APIRouter()


@router.post("/embeddings", response_model=EmbeddingResponse)
async def create_embeddings(
    body: EmbeddingRequest,
    provider: EmbeddingProvider = Depends(get_embedding_provider),
) -> EmbeddingResponse:
    """Generate vector embeddings for the given texts."""
    results = await provider.embed_texts(body.texts)

    embeddings = [
        EmbeddingData(
            index=i,
            vector=r.vector,
            token_count=r.token_count,
        )
        for i, r in enumerate(results)
    ]

    total_tokens = sum(r.token_count for r in results)

    return EmbeddingResponse(
        model=provider.model_name(),
        dimension=provider.dimension(),
        embeddings=embeddings,
        total_tokens=total_tokens,
    )
