"""Shared test fixtures for RAG service tests."""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from rag_service.config import Settings
from rag_service.main import create_app


@pytest.fixture
def settings() -> Settings:
    """Create test settings with defaults."""
    return Settings()


@pytest.fixture
def app(settings: Settings):
    """Create a test FastAPI application."""
    return create_app(settings)


@pytest.fixture
def client(app) -> TestClient:
    """Create a test client."""
    return TestClient(app)
