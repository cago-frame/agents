"""Pydantic schemas for the embeddings API."""

from __future__ import annotations

from pydantic import BaseModel, Field


class EmbeddingRequest(BaseModel):
    """Request body for generating embeddings."""

    texts: list[str] = Field(..., min_length=1, description="List of texts to embed")


class EmbeddingData(BaseModel):
    """Single embedding result."""

    index: int
    vector: list[float]
    token_count: int = 0


class EmbeddingResponse(BaseModel):
    """Response body for embedding generation."""

    model: str
    dimension: int
    embeddings: list[EmbeddingData]
    total_tokens: int = 0
