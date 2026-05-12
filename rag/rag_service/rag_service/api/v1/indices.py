"""Indices API endpoints: CRUD for vector indices."""

from __future__ import annotations

from fastapi import APIRouter, Depends

from rag_service.api.v1.schemas.indices import (
    CreateIndexRequest,
    DeleteIndexResponse,
    IndexInfo,
    IndexListResponse,
)
from rag_service.dependencies import get_vector_store
from rag_service.vectorstore.base import VectorStore

router = APIRouter()


@router.post("/indices", response_model=IndexInfo, status_code=201)
async def create_index(
    body: CreateIndexRequest,
    store: VectorStore = Depends(get_vector_store),
) -> IndexInfo:
    """Create a new vector index."""
    await store.create_index(
        index_name=body.name,
        dimension=body.dimension,
        similarity=body.similarity,
    )
    return IndexInfo(
        name=body.name,
        display_name=body.name,
        docs_count=0,
        store_size="0b",
        health="green",
        status="open",
    )


@router.get("/indices", response_model=IndexListResponse)
async def list_indices(
    store: VectorStore = Depends(get_vector_store),
) -> IndexListResponse:
    """List all managed vector indices."""
    indices = await store.list_indices()
    items = [IndexInfo(**idx) for idx in indices]
    return IndexListResponse(indices=items, total=len(items))


@router.delete("/indices/{name}", response_model=DeleteIndexResponse)
async def delete_index(
    name: str,
    store: VectorStore = Depends(get_vector_store),
) -> DeleteIndexResponse:
    """Delete a vector index and all its documents."""
    await store.delete_index(name)
    return DeleteIndexResponse(message="Index deleted successfully", index=name)
