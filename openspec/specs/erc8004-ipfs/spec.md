## ADDED Requirements

### Requirement: IPFS provider abstraction

The system SHALL provide an `IpfsProvider` abstract base class with a single async method `upload(data: dict) -> str` that returns a URI string.

#### Scenario: Upload returns URI
- **WHEN** `upload(data)` is called with a JSON-serializable dict
- **THEN** the provider SHALL return a non-empty URI string identifying the uploaded content

### Requirement: Mock IPFS implementation

The system SHALL provide a `MockIpfs` implementation that writes JSON to a temporary file and returns a `mock://` URI without making any network calls.

#### Scenario: Mock upload and retrieve round trip
- **WHEN** `MockIpfs.upload(data)` is called
- **THEN** it SHALL write the data as JSON to a temp file and return a `mock://<path>` URI

#### Scenario: Mock retrieve
- **WHEN** `MockIpfs.retrieve(uri)` is called with a `mock://` URI
- **THEN** it SHALL read the file and return the original dict

### Requirement: Pinata IPFS implementation

The system SHALL provide a `PinataIpfs` implementation that POSTs JSON to the Pinata API using an aiohttp session and returns an `ipfs://<CID>` URI.

#### Scenario: Successful Pinata upload
- **WHEN** `PinataIpfs.upload(data)` is called and Pinata returns HTTP 200 with a CID
- **THEN** it SHALL return `ipfs://<CID>`

#### Scenario: Pinata upload failure raises
- **WHEN** the Pinata API returns a non-200 status
- **THEN** `upload` SHALL raise an exception with the HTTP status code
