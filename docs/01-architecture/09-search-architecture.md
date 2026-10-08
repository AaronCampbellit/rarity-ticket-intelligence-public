# Search Architecture

**Status:** Draft

Universal search spans authorized tickets, clients, contacts, assets, services, comments, attachments, knowledge, projects, contracts, identifiers, IP addresses, serial numbers, and other indexed objects.

Search indexing is event-driven and treats authorization as a hard boundary. Documents carry MSP/client scope and visibility metadata, but the query path must still enforce current authorization. Index lag is measured and visible. Deleted or permission-changed records are removed or resecured promptly.

Exact identifier lookup, structured filters, full-text relevance, and later semantic retrieval share one coherent interface. Attachment extraction is sandboxed and subject to type, size, malware, and retention controls.
