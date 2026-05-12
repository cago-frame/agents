"""Elasticsearch vector store implementation."""

from __future__ import annotations

import logging
from typing import Any

from elasticsearch import AsyncElasticsearch, NotFoundError

from rag_service.config import ElasticsearchConfig
from rag_service.utils.errors import IndexNotFoundError, VectorStoreError
from rag_service.vectorstore.base import SearchResult, VectorStore

logger = logging.getLogger(__name__)


class ElasticsearchVectorStore(VectorStore):
    """Vector store backed by Elasticsearch with dense_vector + kNN search."""

    def __init__(self, config: ElasticsearchConfig):
        self._config = config
        self._prefix = config.default_index_prefix
        self._similarity = config.similarity
        self._client: AsyncElasticsearch | None = None

    async def initialize(self) -> None:
        """Create the Elasticsearch async client."""
        kwargs: dict[str, Any] = {
            "hosts": self._config.hosts,
            "verify_certs": self._config.verify_certs,
        }
        if self._config.password:
            kwargs["basic_auth"] = (self._config.username, self._config.password)
        if self._config.ca_certs:
            kwargs["ca_certs"] = self._config.ca_certs
        if self._config.client_cert:
            kwargs["client_cert"] = self._config.client_cert
        if self._config.client_key:
            kwargs["client_key"] = self._config.client_key

        self._client = AsyncElasticsearch(**kwargs)
        # Verify connection
        try:
            info = await self._client.info()
            logger.info("Connected to Elasticsearch %s", info["version"]["number"])
        except Exception as e:
            logger.warning("Could not connect to Elasticsearch: %s", e)

    @property
    def client(self) -> AsyncElasticsearch:
        if self._client is None:
            raise VectorStoreError("Elasticsearch client not initialized")
        return self._client

    def _full_index_name(self, index_name: str) -> str:
        """Apply prefix to index name if not already present."""
        if index_name.startswith(self._prefix):
            return index_name
        return f"{self._prefix}{index_name}"

    # --- Index management ---

    async def create_index(
        self,
        index_name: str,
        dimension: int,
        similarity: str = "",
    ) -> None:
        """Create a new ES index with dense_vector mapping."""
        full_name = self._full_index_name(index_name)
        sim = similarity or self._similarity

        # Map similarity names to ES similarity types
        sim_map = {
            "cosine": "cosine",
            "dot_product": "dot_product",
            "l2_norm": "l2_norm",
            "euclidean": "l2_norm",
        }
        es_similarity = sim_map.get(sim, "cosine")

        mappings = {
            "properties": {
                "content": {"type": "text"},
                "vector": {
                    "type": "dense_vector",
                    "dims": dimension,
                    "index": True,
                    "similarity": es_similarity,
                },
                "metadata": {"type": "object", "enabled": True},
                "document_id": {"type": "keyword"},
                "chunk_index": {"type": "integer"},
            }
        }

        settings = {
            "number_of_shards": 1,
            "number_of_replicas": 0,
        }

        try:
            await self.client.indices.create(
                index=full_name,
                mappings=mappings,
                settings=settings,
            )
            logger.info("Created index: %s (dim=%d, similarity=%s)", full_name, dimension, es_similarity)
        except Exception as e:
            raise VectorStoreError(f"Failed to create index '{full_name}': {e}") from e

    async def delete_index(self, index_name: str) -> None:
        full_name = self._full_index_name(index_name)
        try:
            await self.client.indices.delete(index=full_name)
            logger.info("Deleted index: %s", full_name)
        except NotFoundError:
            raise IndexNotFoundError(index_name)
        except Exception as e:
            raise VectorStoreError(f"Failed to delete index '{full_name}': {e}") from e

    async def list_indices(self) -> list[dict[str, Any]]:
        try:
            resp = await self.client.cat.indices(index=f"{self._prefix}*", format="json")
            results = []
            for idx in resp:
                name = idx.get("index", "")
                results.append(
                    {
                        "name": name,
                        "display_name": name[len(self._prefix) :] if name.startswith(self._prefix) else name,
                        "docs_count": int(idx.get("docs.count", 0)),
                        "store_size": idx.get("store.size", "0b"),
                        "health": idx.get("health", "unknown"),
                        "status": idx.get("status", "unknown"),
                    }
                )
            return results
        except Exception as e:
            raise VectorStoreError(f"Failed to list indices: {e}") from e

    async def index_exists(self, index_name: str) -> bool:
        full_name = self._full_index_name(index_name)
        try:
            return await self.client.indices.exists(index=full_name)
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
        full_name = self._full_index_name(index_name)
        doc = {
            "content": content,
            "vector": vector,
            "metadata": metadata or {},
        }

        # Extract document_id and chunk_index from ID format {doc_id}_{chunk_idx}
        if "_" in document_id:
            parts = document_id.rsplit("_", 1)
            doc["document_id"] = parts[0]
            try:
                doc["chunk_index"] = int(parts[1])
            except ValueError:
                doc["document_id"] = document_id
        else:
            doc["document_id"] = document_id

        try:
            await self.client.index(index=full_name, id=document_id, document=doc)
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

        full_name = self._full_index_name(index_name)
        operations: list[dict[str, Any]] = []
        ids: list[str] = []

        for doc in documents:
            doc_id = doc["id"]
            ids.append(doc_id)

            body = {
                "content": doc["content"],
                "vector": doc["vector"],
                "metadata": doc.get("metadata", {}),
            }

            # Extract document_id and chunk_index
            if "_" in doc_id:
                parts = doc_id.rsplit("_", 1)
                body["document_id"] = parts[0]
                try:
                    body["chunk_index"] = int(parts[1])
                except ValueError:
                    body["document_id"] = doc_id
            else:
                body["document_id"] = doc_id

            operations.append({"index": {"_index": full_name, "_id": doc_id}})
            operations.append(body)

        try:
            resp = await self.client.bulk(operations=operations, refresh="wait_for")
            if resp.get("errors"):
                failed = [
                    item["index"]["error"]["reason"]
                    for item in resp["items"]
                    if "error" in item.get("index", {})
                ]
                if failed:
                    logger.error("Bulk insert errors: %s", failed[:5])
                    raise VectorStoreError(f"Bulk insert had {len(failed)} errors: {failed[0]}")
            return ids
        except VectorStoreError:
            raise
        except Exception as e:
            raise VectorStoreError(f"Failed to batch insert: {e}") from e

    async def delete(self, index_name: str, document_id: str) -> None:
        full_name = self._full_index_name(index_name)
        try:
            await self.client.delete(index=full_name, id=document_id, refresh="wait_for")
        except NotFoundError:
            pass  # Already deleted, idempotent
        except Exception as e:
            raise VectorStoreError(f"Failed to delete document: {e}") from e

    async def delete_by_metadata(
        self,
        index_name: str,
        filters: dict[str, Any],
    ) -> int:
        full_name = self._full_index_name(index_name)

        # Build ES query from metadata filters
        must_clauses = []
        for key, value in filters.items():
            if key == "document_id":
                must_clauses.append({"term": {"document_id": value}})
            else:
                must_clauses.append({"term": {f"metadata.{key}": value}})

        query = {"bool": {"must": must_clauses}} if must_clauses else {"match_all": {}}

        try:
            resp = await self.client.delete_by_query(
                index=full_name,
                query=query,
                refresh=True,
            )
            deleted = resp.get("deleted", 0)
            logger.info("Deleted %d documents from %s matching %s", deleted, full_name, filters)
            return deleted
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
        full_name = self._full_index_name(index_name)

        # Build kNN query
        knn: dict[str, Any] = {
            "field": "vector",
            "query_vector": query_vector,
            "k": top_k,
            "num_candidates": top_k * 2,
        }

        # Add filters
        if filters:
            filter_clauses = []
            for key, value in filters.items():
                if key == "document_id":
                    filter_clauses.append({"term": {"document_id": value}})
                else:
                    filter_clauses.append({"term": {f"metadata.{key}": value}})
            knn["filter"] = {"bool": {"must": filter_clauses}}

        try:
            resp = await self.client.search(
                index=full_name,
                knn=knn,
                size=top_k,
                source=["content", "metadata", "document_id", "chunk_index"],
            )

            results = []
            for hit in resp["hits"]["hits"]:
                score = hit.get("_score", 0.0)
                if score < min_score:
                    continue

                source = hit["_source"]
                results.append(
                    SearchResult(
                        document_id=hit["_id"],
                        score=score,
                        content=source.get("content", ""),
                        metadata=source.get("metadata", {}),
                    )
                )

            return results
        except Exception as e:
            raise VectorStoreError(f"Search failed: {e}") from e

    # --- Lifecycle ---

    async def close(self) -> None:
        if self._client is not None:
            await self._client.close()
            self._client = None
