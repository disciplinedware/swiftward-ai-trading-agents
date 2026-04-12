"""IPFS provider abstraction for ERC-8004 off-chain data storage."""
import json
import tempfile
from abc import ABC, abstractmethod
from pathlib import Path


class IpfsProvider(ABC):
    """Abstract IPFS provider — upload JSON, get back a URI."""

    @abstractmethod
    async def upload(self, data: dict, filename: str = "data.json") -> str:
        """Upload a JSON-serializable dict and return the content URI."""

    @staticmethod
    def content_hash(data: dict) -> bytes:
        """Return Ethereum keccak256 hash of the canonical JSON encoding as 32 bytes.

        Uses eth_hash (bundled with web3) which implements keccak256, NOT NIST SHA-3.
        The two algorithms produce different digests for the same input.
        """
        from eth_hash.auto import keccak  # part of web3 package

        raw = json.dumps(data, sort_keys=True, separators=(",", ":")).encode()
        return keccak(raw)


class MockIpfs(IpfsProvider):
    """Local mock IPFS — writes to a temp file, no network calls.

    URI format: mock://<absolute_path>
    """

    async def upload(self, data: dict, filename: str = "data.json") -> str:
        fd, path = tempfile.mkstemp(suffix=".json", prefix="mock_ipfs_")
        with open(fd, "w") as f:
            json.dump(data, f)
        return f"mock://{path}"

    async def retrieve(self, uri: str) -> dict:
        if not uri.startswith("mock://"):
            raise ValueError(f"MockIpfs can only retrieve mock:// URIs, got: {uri!r}")
        path = Path(uri[len("mock://"):])
        return json.loads(path.read_text())


class PinataIpfs(IpfsProvider):
    """Pinata IPFS provider — uploads via Pinata V3 Files API.

    Requires a Pinata JWT token with Files: Write scope.
    """

    _PINATA_URL = "https://uploads.pinata.cloud/v3/files"

    def __init__(self, jwt: str) -> None:
        self._jwt = jwt

    async def upload(self, data: dict, filename: str = "data.json") -> str:
        import aiohttp  # deferred import — only needed in prod

        headers = {
            "Authorization": f"Bearer {self._jwt}",
        }
        form = aiohttp.FormData()
        raw = json.dumps(data, sort_keys=True, separators=(",", ":")).encode()
        form.add_field("network", "public")
        form.add_field("file", raw, filename=filename, content_type="application/json")
        async with aiohttp.ClientSession() as session:
            async with session.post(
                self._PINATA_URL, data=form, headers=headers
            ) as resp:
                if resp.status != 200:
                    text = await resp.text()
                    raise RuntimeError(
                        f"Pinata upload failed: HTTP {resp.status} — {text}"
                    )
                body = await resp.json()
                cid = body["data"]["cid"]
        return f"ipfs://{cid}"
