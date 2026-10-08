# Object Storage

**Status:** Draft

MinIO is the first supported self-hosted S3-compatible object-storage option. It stores attachments, raw inbound payloads, exports, and selected backup artifacts. PostgreSQL remains authoritative for metadata, permissions, checksums, lifecycle, and relationships. Storage planning forecasts seven years of email/attachment retention from observed intake volume and maintains 30% headroom; it warns at 70% capacity and is critical at 85%.

An initial single-server installation may run MinIO as a Compose service with dedicated persistent storage. HA production uses storage outside the application host or an independently durable/HA MinIO deployment. Uploads are size/type bounded, streamed, checksum-verified, and inaccessible by guessable public URL. V1 has no Rarity malware scanner; downloads are authorized at request time and use short-lived delivery grants. Retention and deletion honor legal and recovery policy.

The API requires an explicit `S3_BUCKET`, `S3_REGION`, `S3_ACCESS_KEY_ID`, and `S3_SECRET_ACCESS_KEY` in addition to `S3_ENDPOINT`. Pilot and production credentials are provisioned outside the application with access limited to the attachment bucket; the API validates that the bucket already exists at startup. Local/demo startup may create the configured bucket using generated local-only MinIO credentials.

The authenticated Setup Center displays the effective endpoint, bucket, region,
and credential-presence state without returning credentials. **Test object
storage** performs a bounded write/delete probe under `setup-probes/`; cleanup
is mandatory and durable evidence contains only the safe result and expiry.
Missing values are corrected in the deployment environment and require an API
restart.
