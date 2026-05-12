"""Pydantic schemas for the search API."""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field


class SearchRequest(BaseModel):
    """Request body for semantic search."""

    query: str = Field(..., min_length=1, description="Search query text")
    index_name: str = Field(..., min_length=1, description="Name of the index to search")
    top_k: int = Field(default=10, ge=1, le=100, description="Number of results to return")
    min_score: float = Field(default=0.0, ge=0.0, le=1.0, description="Minimum similarity score threshold")
    filters: dict[str, Any] | None = Field(default=None, description="Metadata filters")


class SearchHit(BaseModel):
    """A single search result."""

    document_id: str
    score: float
    content: str
    metadata: dict[str, Any] = Field(default_factory=dict)


class SearchResponse(BaseModel):
    """Response body for semantic search."""

    query: str
    index_name: str
    hits: list[SearchHit]
    total: int
