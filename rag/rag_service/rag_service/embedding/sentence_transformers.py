"""Sentence Transformers embedding provider (local models)."""

from __future__ import annotations

import asyncio
import logging
from functools import partial

from rag_service.config import SentenceTransformersConfig
from rag_service.embedding.base import EmbeddingProvider, EmbeddingResult
from rag_service.utils.errors import EmbeddingError

logger = logging.getLogger(__name__)


class SentenceTransformersProvider(EmbeddingProvider):
    """Embedding provider using local Sentence Transformers models.

    CPU/GPU bound operations are run in an executor to avoid blocking the event loop.
    Requires: pip install rag-service[local]
    """

    def __init__(self, config: SentenceTransformersConfig):
        from sentence_transformers import SentenceTransformer

        self._config = config
        self._dimensions = config.dimensions
        try:
            self._model = SentenceTransformer(config.model_name, device=config.device)
            # Update dimensions from actual model if possible
            test_embedding = self._model.encode(["test"])
            if test_embedding is not None and len(test_embedding) > 0:
                self._dimensions = len(test_embedding[0])
                logger.info(
                    "Loaded model '%s' on %s (dim=%d)",
                    config.model_name,
                    config.device,
                    self._dimensions,
                )
        except Exception as e:
            raise EmbeddingError(f"Failed to load Sentence Transformers model '{config.model_name}': {e}") from e

    async def embed_texts(self, texts: list[str]) -> list[EmbeddingResult]:
        if not texts:
            return []

        loop = asyncio.get_event_loop()
        try:
            embeddings = await loop.run_in_executor(None, partial(self._encode, texts))
        except Exception as e:
            logger.error("Sentence Transformers embedding error: %s", e)
            raise EmbeddingError(f"Sentence Transformers error: {e}") from e

        return [EmbeddingResult(vector=emb.tolist(), token_count=0) for emb in embeddings]

    def _encode(self, texts: list[str]):
        """Synchronous encoding (runs in executor)."""
        return self._model.encode(texts, show_progress_bar=False)

    def dimension(self) -> int:
        return self._dimensions

    def model_name(self) -> str:
        return self._config.model_name
