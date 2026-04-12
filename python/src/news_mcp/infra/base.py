from typing import Protocol, TypedDict


class NewsPost(TypedDict):
    title: str
    description: str
    url: str
    published_at: str
    source: str


class NewsClient(Protocol):
    async def get_posts(self, currencies: list[str], limit: int = 50) -> list[NewsPost]: ...
