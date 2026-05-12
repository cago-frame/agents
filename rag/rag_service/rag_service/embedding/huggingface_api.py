"""HuggingFace Inference API embedding provider."""

from __future__ import annotations

import logging

import httpx

from rag_service.config import HuggingFaceAPIConfig
from rag_service.embedding.base import EmbeddingProvider, EmbeddingResult
from rag_service.utils.errors import EmbeddingError

logger = logging.getLogger(__name__)


class HuggingFaceAPIProvider(EmbeddingProvider):
    """Embedding provider using HuggingFace Inference API.

    Uses httpx for async HTTP calls to the HuggingFace inference endpoint.
    """

    def __init__(self, config: HuggingFaceAPIConfig):
        self._config = config
        self._dimensions = config.dimensions
        self._client = httpx.AsyncClient(
            timeout=httpx.Timeout(60.0),
            headers={"Authorization": f"Bearer {config.api_key}"},
        )
        self._url = f"{config.api_url}/{config.model}"

    async def embed_texts(self, texts: list[str]) -> list[EmbeddingResult]:
        if not texts:
            return []

        try:
            response = await self._client.post(
                self._url,
                json={"inputs": texts, "options": {"wait_for_model": True}},
            )
            response.raise_for_status()
            data = response.json()

            # HuggingFace returns a list of embeddings (list of lists of floats)
            if not isinstance(data, list):
                raise EmbeddingError(f"Unexpected response format from HuggingFace API: {type(data)}")

            results = []
            for embedding in data:
                if isinstance(embedding, list) and len(embedding) > 0:
                    # Handle nested list (batch response)
                    if isinstance(embedding[0], list):
                        # Some models return [[...]] per input
                        embedding = embedding[0]
                    results.append(EmbeddingResult(vector=embedding, token_count=0))

            # Update dimensions from actual response
            if results and self._dimensions == 0:
                self._dimensions = len(results[0].vector)

            return results

        except httpx.HTTPStatusError as e:
            logger.error("HuggingFace API HTTP error: %s %s", e.response.status_code, e.response.text)
            raise EmbeddingError(f"HuggingFace API error ({e.response.status_code}): {e.response.text}") from e
        except EmbeddingError:
            raise
        except Exception as e:
            logger.error("HuggingFace API error: %s", e)
            raise EmbeddingError(f"HuggingFace API error: {e}") from e

    def dimension(self) -> int:
        return self._dimensions

    def model_name(self) -> str:
        return self._config.model
