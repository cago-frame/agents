"""OpenAI embedding provider implementation."""

from __future__ import annotations

import logging

from openai import AsyncOpenAI

from rag_service.config import OpenAIEmbeddingConfig
from rag_service.embedding.base import EmbeddingProvider, EmbeddingResult
from rag_service.utils.errors import EmbeddingError

logger = logging.getLogger(__name__)


class OpenAIEmbeddingProvider(EmbeddingProvider):
    """Embedding provider using OpenAI API (or compatible endpoints)."""

    def __init__(self, config: OpenAIEmbeddingConfig):
        self._config = config
        self._client = AsyncOpenAI(
            api_key=config.api_key,
            base_url=config.base_url,
        )
        self._model = config.model
        self._dimensions = config.dimensions
        self._batch_size = config.batch_size

    async def embed_texts(self, texts: list[str]) -> list[EmbeddingResult]:
        """Generate embeddings using the OpenAI API.

        Handles batching automatically for large input lists.
        """
        if not texts:
            return []

        all_results: list[EmbeddingResult] = []

        try:
            for i in range(0, len(texts), self._batch_size):
                batch = texts[i : i + self._batch_size]
                response = await self._client.embeddings.create(
                    input=batch,
                    model=self._model,
                    dimensions=self._dimensions,
                )

                total_tokens = getattr(response.usage, "total_tokens", 0) if response.usage else 0
                per_item_tokens = total_tokens // len(batch) if total_tokens and len(batch) > 0 else 0

                for item in response.data:
                    all_results.append(
                        EmbeddingResult(
                            vector=item.embedding,
                            token_count=per_item_tokens,
                        )
                    )

        except Exception as e:
            logger.error("OpenAI embedding error: %s", e)
            raise EmbeddingError(f"OpenAI API error: {e}") from e

        return all_results

    def dimension(self) -> int:
        return self._dimensions

    def model_name(self) -> str:
        return self._model
