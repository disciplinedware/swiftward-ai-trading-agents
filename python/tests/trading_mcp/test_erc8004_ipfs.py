"""Tests for IPFS provider implementations."""
import json

import pytest

from trading_mcp.erc8004.ipfs import IpfsProvider, MockIpfs, PinataIpfs

# ---------------------------------------------------------------------------
# MockIpfs
# ---------------------------------------------------------------------------


async def test_mock_upload_returns_mock_uri():
    ipfs = MockIpfs()
    uri = await ipfs.upload({"key": "value"})
    assert uri.startswith("mock://")
    assert len(uri) > len("mock://")


async def test_mock_upload_and_retrieve_round_trip():
    ipfs = MockIpfs()
    data = {"asset": "ETH", "price": "2000.00", "score": 42}
    uri = await ipfs.upload(data)
    retrieved = await ipfs.retrieve(uri)
    assert retrieved == data


async def test_mock_retrieve_raises_on_non_mock_uri():
    ipfs = MockIpfs()
    with pytest.raises(ValueError, match="mock://"):
        await ipfs.retrieve("ipfs://Qm123")


async def test_content_hash_is_deterministic():
    data = {"a": 1, "b": 2}
    h1 = IpfsProvider.content_hash(data)
    h2 = IpfsProvider.content_hash(data)
    assert h1 == h2
    assert len(h1) == 32


async def test_content_hash_differs_for_different_data():
    h1 = IpfsProvider.content_hash({"x": 1})
    h2 = IpfsProvider.content_hash({"x": 2})
    assert h1 != h2


# ---------------------------------------------------------------------------
# PinataIpfs
# ---------------------------------------------------------------------------


@pytest.mark.parametrize(
    "name,status,body,expected_uri,raises",
    [
        (
            "successful upload",
            200,
            json.dumps({"data": {"cid": "QmTestCID123", "id": "abc"}}),
            "ipfs://QmTestCID123",
            False,
        ),
        (
            "HTTP 401 raises",
            401,
            "Unauthorized",
            None,
            True,
        ),
        (
            "HTTP 500 raises",
            500,
            "Internal Server Error",
            None,
            True,
        ),
    ],
)
async def test_pinata_upload(name, status, body, expected_uri, raises):
    from unittest.mock import AsyncMock, MagicMock, patch

    mock_resp = MagicMock()
    mock_resp.status = status
    mock_resp.text = AsyncMock(return_value=body)
    mock_resp.json = AsyncMock(return_value=json.loads(body) if status == 200 else {})
    mock_resp.__aenter__ = AsyncMock(return_value=mock_resp)
    mock_resp.__aexit__ = AsyncMock(return_value=False)

    mock_session = MagicMock()
    mock_session.post = MagicMock(return_value=mock_resp)
    mock_session.__aenter__ = AsyncMock(return_value=mock_session)
    mock_session.__aexit__ = AsyncMock(return_value=False)

    with patch("aiohttp.ClientSession", return_value=mock_session):
        ipfs = PinataIpfs(jwt="test-jwt")
        if raises:
            with pytest.raises(RuntimeError, match="Pinata upload failed"):
                await ipfs.upload({"test": "data"})
        else:
            uri = await ipfs.upload({"test": "data"})
            assert uri == expected_uri, name
