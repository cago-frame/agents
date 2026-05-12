"""ChromaDB vector store implementation."""

from __future__ import annotations

import asyncio
import logging
from functools import partial
from typing import Any

from rag_service.config import ChromaConfig
from rag_service.utils.errors import IndexNotFoundError, VectorStoreError
from rag_service.vectorstore.base import SearchResult, VectorStore

logger = logging.getLogger(__name__)


class ChromaVectorStore(VectorStore):
    """Vector store backed by ChromaDB with PersistentClient."""

    def __init__(self, config: ChromaConfig):
        self._config = config
        self._prefix = config.collection_prefix
        self._client: Any = None  # chromadb.PersistentClient

    async def _run_sync(self, fn: Any, *args: Any, **kwargs: Any) -> Any:
        """Run a synchronous ChromaDB call in a thread executor."""
        loop = asyncio.get_event_loop()
        return await loop.run_in_executor(None, partial(fn, *args, **kwargs))

    async def initialize(self) -> None:
        """Create the ChromaDB PersistentClient."""
        try:
            import chromadb
        except ImportError:
            raise VectorStoreError(
                "ChromaDB is not installed. Install with: pip install rag-service[local] "
                "or: pip install chromadb>=0.5.0"
            )

        self._client = await self._run_sync(
            chromadb.PersistentClient,
            path=self._config.persist_directory,
        )
        logger.info("ChromaDB initialized at: %s", self._config.persist_directory)

    @property
    def client(self) -> Any:
        if self._client is None:
            raise VectorStoreError("ChromaDB client not initialized")
        return self._client

    def _full_collection_name(self, index_name: str) -> str:
        """Apply prefix to collection name if not already present."""
        if index_name.startswith(self._prefix):
            return index_name
        return f"{self._prefix}{index_name}"

    # --- Index management ---

    async def create_index(
        self,
        index_name: str,
        dimension: int,
        similarity: str = "cosine",
    ) -> None:
        """Create or get a ChromaDB collection."""
        collection_name = self._full_collection_name(index_name)

        # Map similarity to ChromaDB hnsw:space values
        sim_map = {
            "cosine": "cosine",
            "dot_product": "ip",
            "l2_norm": "l2",
            "euclidean": "l2",
        }
        space = sim_map.get(similarity, "cosine")

        try:
            await self._run_sync(
                self.client.get_or_create_collection,
                name=collection_name,
                metadata={"hnsw:space": space},
            )
            logger.info(
                "Created/verified collection: %s (space=%s)", collection_name, space
            )
        except Exception as e:
            raise VectorStoreError(
                f"Failed to create collection '{collection_name}': {e}"
            ) from e

    async def delete_index(self, index_name: str) -> None:
        collection_name = self._full_collection_name(index_name)
        try:
            await self._run_sync(self.client.delete_collection, name=collection_name)
            logger.info("Deleted collection: %s", collection_name)
        except Exception as e:
            error_msg = str(e).lower()
            if "not found" in error_msg or "does not exist" in error_msg:
                raise IndexNotFoundError(index_name)
            raise VectorStoreError(
                f"Failed to delete collection '{collection_name}': {e}"
            ) from e

    async def list_indices(self) -> list[dict[str, Any]]:
        try:
            collections = await self._run_sync(self.client.list_collections)
            results = []
            for col in collections:
                name = col if isinstance(col, str) else col.name
                if not name.startswith(self._prefix):
                    continue
                # Get collection to count documents
                collection = await self._run_sync(
                    self.client.get_collection, name=name
                )
                count = await self._run_sync(collection.count)
                results.append(
                    {
                        "name": name,
                        "display_name": name[len(self._prefix) :]
                        if name.startswith(self._prefix)
                        else name,
                        "docs_count": count,
                        "store_size": "N/A",
                        "health": "green",
                        "status": "open",
                    }
                )
            return results
        except Exception as e:
            raise VectorStoreError(f"Failed to list collections: {e}") from e

    async def index_exists(self, index_name: str) -> bool:
        collection_name = self._full_collection_name(index_name)
        try:
            collections = await self._run_sync(self.client.list_collections)
            for col in collections:
                name = col if isinstance(col, str) else col.name
                if name == collection_name:
                    return True
            return False
        except Exception:
            return False

    # --- Document CRUD ---

    async def insert(
        self,
        index_name: str,
        document_id: str,
        content: str,
        vector: list[float],
        metadata: dict[str, Any] | None = None,
    ) -> str:
        collection_name = self._full_collection_name(index_name)
        try:
            collection = await self._run_sync(
                self.client.get_collection, name=collection_name
            )
            # ChromaDB metadata values must be str, int, float, or bool
            safe_metadata = self._sanitize_metadata(metadata or {})
            await self._run_sync(
                collection.upsert,
                ids=[document_id],
                documents=[content],
                embeddings=[vector],
                metadatas=[safe_metadata],
            )
            return document_id
        except Exception as e:
            raise VectorStoreError(f"Failed to insert document: {e}") from e

    async def batch_insert(
        self,
        index_name: str,
        documents: list[dict[str, Any]],
    ) -> list[str]:
        if not documents:
            return []

        collection_name = self._full_collection_name(index_name)
        try:
            collection = await self._run_sync(
                self.client.get_collection, name=collection_name
            )

            ids = [doc["id"] for doc in documents]
            contents = [doc["content"] for doc in documents]
            embeddings = [doc["vector"] for doc in documents]
            metadatas = [
                self._sanitize_metadata(doc.get("metadata", {})) for doc in documents
            ]

            # ChromaDB has a batch size limit; chunk if needed
            batch_size = 5000
            for i in range(0, len(ids), batch_size):
                end = i + batch_size
                await self._run_sync(
                    collection.upsert,
                    ids=ids[i:end],
                    documents=contents[i:end],
                    embeddings=embeddings[i:end],
                    metadatas=metadatas[i:end],
                )

            return ids
        except Exception as e:
            raise VectorStoreError(f"Failed to batch insert: {e}") from e

    async def delete(self, index_name: str, document_id: str) -> None:
        collection_name = self._full_collection_name(index_name)
        try:
            collection = await self._run_sync(
                self.client.get_collection, name=collection_name
            )
            await self._run_sync(collection.delete, ids=[document_id])
        except Exception as e:
            error_msg = str(e).lower()
            if "not found" in error_msg:
                pass  # Idempotent delete
            else:
                raise VectorStoreError(f"Failed to delete document: {e}") from e

    async def delete_by_metadata(
        self,
        index_name: str,
        filters: dict[str, Any],
    ) -> int:
        collection_name = self._full_collection_name(index_name)
        try:
            collection = await self._run_sync(
                self.client.get_collection, name=collection_name
            )

            # Build ChromaDB where clause
            where = self._build_where_clause(filters)
            if not where:
                return 0

            # Get matching IDs first
            results = await self._run_sync(
                collection.get,
                where=where,
            )
            matched_ids = results.get("ids", []) if isinstance(results, dict) else results.ids if hasattr(results, 'ids') else []
            if not matched_ids:
                return 0

            await self._run_sync(collection.delete, ids=matched_ids)
            logger.info(
                "Deleted %d documents from %s matching %s",
                len(matched_ids),
                collection_name,
                filters,
            )
            return len(matched_ids)
        except Exception as e:
            raise VectorStoreError(f"Failed to delete by metadata: {e}") from e

    # --- Search ---

    async def search(
        self,
        index_name: str,
        query_vector: list[float],
        top_k: int = 10,
        min_score: float = 0.0,
        filters: dict[str, Any] | None = None,
    ) -> list[SearchResult]:
        collection_name = self._full_collection_name(index_name)
        try:
            collection = await self._run_sync(
                self.client.get_collection, name=collection_name
            )

            query_kwargs: dict[str, Any] = {
                "query_embeddings": [query_vector],
                "n_results": top_k,
            }
            if filters:
                where = self._build_where_clause(filters)
                if where:
                    query_kwargs["where"] = where

            raw = await self._run_sync(collection.query, **query_kwargs)

            results = []
            if raw and raw.get("ids") and raw["ids"][0]:
                ids = raw["ids"][0]
                documents = raw.get("documents", [[]])[0]
                metadatas = raw.get("metadatas", [[]])[0]
                distances = raw.get("distances", [[]])[0]

                for i, doc_id in enumerate(ids):
                    # ChromaDB returns distances; convert to similarity score
                    # For cosine: similarity = 1 - distance
                    distance = distances[i] if i < len(distances) else 0.0
                    score = 1.0 - distance

                    if score < min_score:
                        continue

                    content = documents[i] if i < len(documents) else ""
                    metadata = metadatas[i] if i < len(metadatas) else {}

                    results.append(
                        SearchResult(
                            document_id=doc_id,
                            score=score,
                            content=content,
                            metadata=metadata or {},
                        )
                    )

            return results
        except Exception as e:
            raise VectorStoreError(f"Search failed: {e}") from e

    # --- Lifecycle ---

    async def close(self) -> None:
        """ChromaDB PersistentClient handles persistence automatically."""
        self._client = None

    # --- Helpers ---

    @staticmethod
    def _sanitize_metadata(metadata: dict[str, Any]) -> dict[str, Any]:
        """Ensure metadata values are ChromaDB-compatible (str, int, float, bool)."""
        sanitized: dict[str, Any] = {}
        for key, value in metadata.items():
            if isinstance(value, (str, int, float, bool)):
                sanitized[key] = value
            elif isinstance(value, list):
                # Convert lists to JSON string
                import json

                sanitized[key] = json.dumps(value)
            elif value is None:
                sanitized[key] = ""
            else:
                sanitized[key] = str(value)
        return sanitized

    @staticmethod
    def _build_where_clause(filters: dict[str, Any]) -> dict[str, Any] | None:
        """Build ChromaDB where clause from filters dict."""
        if not filters:
            return None

        conditions = []
        for key, value in filters.items():
            conditions.append({key: {"$eq": value}})

        if len(conditions) == 1:
            return conditions[0]
        return {"$and": conditions}
