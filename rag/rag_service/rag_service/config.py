"""Configuration management with YAML + environment variable overrides."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any

import yaml
from pydantic import BaseModel, Field
from pydantic_settings import BaseSettings


class ServerConfig(BaseModel):
    host: str = "0.0.0.0"
    port: int = 8000
    log_level: str = "info"


class OpenAIEmbeddingConfig(BaseModel):
    api_key: str = ""
    base_url: str = "https://api.openai.com/v1"
    model: str = "text-embedding-3-small"
    dimensions: int = 1536
    batch_size: int = 100


class SentenceTransformersConfig(BaseModel):
    model_name: str = "all-MiniLM-L6-v2"
    device: str = "cpu"
    dimensions: int = 384


class HuggingFaceAPIConfig(BaseModel):
    api_key: str = ""
    model: str = "sentence-transformers/all-MiniLM-L6-v2"
    api_url: str = "https://api-inference.huggingface.co/pipeline/feature-extraction"
    dimensions: int = 384


class EmbeddingConfig(BaseModel):
    provider: str = "openai"
    openai: OpenAIEmbeddingConfig = Field(default_factory=OpenAIEmbeddingConfig)
    sentence_transformers: SentenceTransformersConfig = Field(default_factory=SentenceTransformersConfig)
    huggingface_api: HuggingFaceAPIConfig = Field(default_factory=HuggingFaceAPIConfig)


class ElasticsearchConfig(BaseModel):
    hosts: list[str] = Field(default_factory=lambda: ["https://localhost:9200"])
    username: str = "elastic"
    password: str = ""
    verify_certs: bool = True
    ca_certs: str = ""  # Path to CA certificate file (e.g. http_ca.crt)
    client_cert: str = ""  # Path to client certificate for mutual TLS
    client_key: str = ""  # Path to client private key for mutual TLS
    default_index_prefix: str = "rag_"
    similarity: str = "cosine"


class ChromaConfig(BaseModel):
    persist_directory: str = "./.chroma_data"
    collection_prefix: str = "rag_"


class VectorStoreConfig(BaseModel):
    provider: str = "elasticsearch"
    elasticsearch: ElasticsearchConfig = Field(default_factory=ElasticsearchConfig)
    chroma: ChromaConfig = Field(default_factory=ChromaConfig)


class ChunkerConfig(BaseModel):
    strategy: str = "recursive_character"
    chunk_size: int = 512
    chunk_overlap: int = 64


class DocumentConfig(BaseModel):
    chunker: ChunkerConfig = Field(default_factory=ChunkerConfig)


class Settings(BaseSettings):
    server: ServerConfig = Field(default_factory=ServerConfig)
    embedding: EmbeddingConfig = Field(default_factory=EmbeddingConfig)
    vectorstore: VectorStoreConfig = Field(default_factory=VectorStoreConfig)
    document: DocumentConfig = Field(default_factory=DocumentConfig)


def _deep_merge(base: dict[str, Any], override: dict[str, Any]) -> dict[str, Any]:
    """Recursively merge override into base dict."""
    result = base.copy()
    for key, value in override.items():
        if key in result and isinstance(result[key], dict) and isinstance(value, dict):
            result[key] = _deep_merge(result[key], value)
        else:
            result[key] = value
    return result


def load_settings(config_path: str | None = None) -> Settings:
    """Load settings from YAML file with environment variable overrides.

    Priority: environment variables > YAML file > defaults.
    """
    yaml_data: dict[str, Any] = {}

    # Determine config file path
    if config_path is None:
        config_path = os.environ.get("RAG_CONFIG_PATH", "config.yaml")

    path = Path(config_path)
    if path.exists():
        with open(path) as f:
            loaded = yaml.safe_load(f)
            if loaded and isinstance(loaded, dict):
                yaml_data = loaded

    # Apply environment variable overrides for common settings
    env_overrides: dict[str, Any] = {}

    # Embedding provider overrides
    if api_key := os.environ.get("OPENAI_API_KEY"):
        env_overrides.setdefault("embedding", {}).setdefault("openai", {})["api_key"] = api_key
    if base_url := os.environ.get("OPENAI_BASE_URL"):
        env_overrides.setdefault("embedding", {}).setdefault("openai", {})["base_url"] = base_url
    if provider := os.environ.get("RAG_EMBEDDING_PROVIDER"):
        env_overrides.setdefault("embedding", {})["provider"] = provider

    # Elasticsearch overrides
    if es_hosts := os.environ.get("ELASTICSEARCH_HOSTS"):
        env_overrides.setdefault("vectorstore", {}).setdefault("elasticsearch", {})["hosts"] = es_hosts.split(",")
    if es_user := os.environ.get("ELASTICSEARCH_USERNAME"):
        env_overrides.setdefault("vectorstore", {}).setdefault("elasticsearch", {})["username"] = es_user
    if es_pass := os.environ.get("ELASTICSEARCH_PASSWORD"):
        env_overrides.setdefault("vectorstore", {}).setdefault("elasticsearch", {})["password"] = es_pass
    if es_ca := os.environ.get("ELASTICSEARCH_CA_CERTS"):
        env_overrides.setdefault("vectorstore", {}).setdefault("elasticsearch", {})["ca_certs"] = es_ca
    if es_cert := os.environ.get("ELASTICSEARCH_CLIENT_CERT"):
        env_overrides.setdefault("vectorstore", {}).setdefault("elasticsearch", {})["client_cert"] = es_cert
    if es_key := os.environ.get("ELASTICSEARCH_CLIENT_KEY"):
        env_overrides.setdefault("vectorstore", {}).setdefault("elasticsearch", {})["client_key"] = es_key

    # Server overrides
    if port := os.environ.get("RAG_PORT"):
        env_overrides.setdefault("server", {})["port"] = int(port)
    if host := os.environ.get("RAG_HOST"):
        env_overrides.setdefault("server", {})["host"] = host
    if log_level := os.environ.get("RAG_LOG_LEVEL"):
        env_overrides.setdefault("server", {})["log_level"] = log_level

    # Vectorstore provider override
    if vs_provider := os.environ.get("RAG_VECTORSTORE_PROVIDER"):
        env_overrides.setdefault("vectorstore", {})["provider"] = vs_provider

    merged = _deep_merge(yaml_data, env_overrides)
    return Settings(**merged)
