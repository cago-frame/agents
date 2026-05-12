"""Search API endpoint: POST /api/v1/search (semantic search)."""

from __future__ import annotations

from fastapi import APIRouter, Depends

from rag_service.api.v1.schemas.search import SearchHit, SearchRequest, SearchResponse
from rag_service.dependencies import get_embedding_provider, get_vector_store
from rag_service.embedding.base import EmbeddingProvider
from rag_service.vectorstore.base import VectorStore

router = APIRouter()


@router.post("/search", response_model=SearchResponse)
async def semantic_search(
    body: SearchRequest,
    provider: EmbeddingProvider = Depends(get_embedding_provider),
    store: VectorStore = Depends(get_vector_store),
) -> SearchResponse:
    """Perform semantic search: embed query text, then kNN search in the vector store."""
    # 1. Embed the query text
    results = await provider.embed_texts([body.query])
    query_vector = results[0].vector

    # 2. Search in the vector store
    hits = await store.search(
        index_name=body.index_name,
        query_vector=query_vector,
        top_k=body.top_k,
        min_score=body.min_score,
        filters=body.filters,
    )

    # 3. Build response
    search_hits = [
        SearchHit(
            document_id=hit.document_id,
            score=hit.score,
            content=hit.content,
            metadata=hit.metadata,
        )
        for hit in hits
    ]

    return SearchResponse(
        query=body.query,
        index_name=body.index_name,
        hits=search_hits,
        total=len(search_hits),
    )
