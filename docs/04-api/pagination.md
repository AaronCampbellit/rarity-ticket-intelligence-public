# Pagination

**Status:** Cursor convention with a documented legacy work-record contract

Large collections use bounded stable pagination. Calendar events, notifications,
and mentions return opaque cursors; clients pass those cursors unchanged.

`GET /api/v1/work-records` retains its array response and keyset parameters:
`limit` (default 50, maximum 100), and paired `before_updated_at`/`before_id`
values from the last row of the previous response. Preserve timestamp precision.
Results sort by `updated_at DESC, id DESC` within the authorized Client.
A full page permits requesting another page; a shorter page is terminal.
Concurrent changes can move records ahead of the cursor; refresh the first page
to observe them and deduplicate accumulated records by ID.

Work-record `status`, `queue_id`, `owner_id`, `priority`, `ownership` (`all`,
`assigned`, `unassigned`) and literal case-insensitive `text` filters apply before
the SQL limit. Text searches display ID, title and description and is limited to
500 bytes. The technician workspace restarts pagination when filters or Client
change and provides an explicit load-more action. Counts describe loaded results,
not the total number of matching tickets.

Future envelope changes must preserve API compatibility. Do not substitute offset
pagination for high-change ticket streams.
