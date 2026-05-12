"""Factory for creating embedding providers based on configuration."""

from __future__ import annotations

from rag_service.config import EmbeddingConfig
from rag_service.embedding.base import EmbeddingProvider
from rag_service.utils.errors import ProviderNotConfiguredError


def create_embedding_provider(config: EmbeddingConfig) -> EmbeddingProvider:
    """Create an embedding provider based on the configuration.

    Uses lazy imports so optional dependencies only need to be installed
    when the corresponding provider is selected.
    """
    provider = config.provider.lower()

    if provider == "openai":
        from rag_service.embedding.openai_provider import OpenAIEmbeddingProvider

        if not config.openai.api_key:
            raise ProviderNotConfiguredError("openai", "api_key is required")
        return OpenAIEmbeddingProvider(config.openai)

    elif provider == "sentence_transformers":
        try:
            from rag_service.embedding.sentence_transformers import SentenceTransformersProvider

            return SentenceTransformersProvider(config.sentence_transformers)
        except ImportError:
            raise ProviderNotConfiguredError(
                "sentence_transformers",
                "Install with: pip install rag-service[local]",
            )

    elif provider == "huggingface_api":
        from rag_service.embedding.huggingface_api import HuggingFaceAPIProvider

        if not config.huggingface_api.api_key:
            raise ProviderNotConfiguredError("huggingface_api", "api_key is required")
        return HuggingFaceAPIProvider(config.huggingface_api)

    else:
        raise ProviderNotConfiguredError(provider, f"Unknown embedding provider: {provider}")
