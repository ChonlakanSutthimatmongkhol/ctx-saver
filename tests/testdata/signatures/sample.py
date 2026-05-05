"""Sample Python file for signatures extraction tests."""
from typing import List, Optional
import os
import asyncio

TIMEOUT = 30
MAX_RETRIES = 3


class BaseHandler:
    """Base class for all handlers."""

    def __init__(self, name: str) -> None:
        self.name = name

    def handle(self, request: dict) -> dict:
        raise NotImplementedError

    async def handle_async(self, request: dict) -> dict:
        raise NotImplementedError


class ConcreteHandler(BaseHandler):
    """A concrete handler implementation."""

    def handle(self, request: dict) -> dict:
        return {"status": "ok", "name": self.name}

    async def handle_async(self, request: dict) -> dict:
        await asyncio.sleep(0)
        return self.handle(request)


def create_handler(name: str) -> BaseHandler:
    """Factory function for handlers."""
    return ConcreteHandler(name)


async def run_pipeline(handlers: List[BaseHandler], request: dict) -> Optional[dict]:
    """Run a list of handlers in sequence."""
    result = None
    for handler in handlers:
        result = await handler.handle_async(request)
    return result


def _internal_helper(value: int) -> int:
    """A private helper function."""
    return value * 2
