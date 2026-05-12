"""Pydantic schemas for the indices API."""

from __future__ import annotations

from pydantic import BaseModel, Field


class CreateIndexRequest(BaseModel):
    """Request body for creating a new vector index."""

    name: str = Field(..., min_length=1, max_length=128, description="Index name (without prefix)")
    dimension: int = Field(..., gt=0, le=4096, description="Vector dimensionality")
    similarity: str = Field(default="cosine", description="Similarity metric: cosine, dot_product, l2_norm")


class IndexInfo(BaseModel):
    """Information about a vector index."""

    name: str
    display_name: str
    docs_count: int = 0
    store_size: str = "0b"
    health: str = "unknown"
    status: str = "unknown"


class IndexListResponse(BaseModel):
    """Response body for listing indices."""

    indices: list[IndexInfo]
    total: int


class DeleteIndexResponse(BaseModel):
    """Response body for deleting an index."""

    message: str
    index: str
