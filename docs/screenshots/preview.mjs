// Documentation preview only: synthetic HTTP responses, no Go server or database.
// Binds to loopback, has no credentials, rejects mutations, and never calls a provider.
import { createServer } from 'node:http';
import { fileURLToPath } from 'node:url';

const { createServer: createViteServer } = await import(new URL('../../frontend/node_modules/vite/dist/node/index.js', import.meta.url));

const root = fileURLToPath(new URL('../../', import.meta.url));
const apiPort = Number(process.env.RTI_PREVIEW_API_PORT || 41731);
const uiPort = Number(process.env.RTI_PREVIEW_UI_PORT || 41732);
for (const port of [apiPort, uiPort]) {
  if (!Number.isInteger(port) || port < 1024 || port > 65535) {
    throw new Error('Preview ports must be integers from 1024 to 65535.');
  }
}
const date = new Date().toISOString().slice(0, 10);
const money = (minor) => ({ minor, currency: 'USD' });
const tag = { id: 'tag-security', label: 'Security', group_id: 'technology', state: 'active', synonyms: [], version: 1 };
const tagAssignment = { id: 'preview-tag-assignment', tag, source: 'human', inherited: false };
const directory = {
  clients: [{ id: 'client-id', display_id: 'CLIENT-001', name: 'Northwind Legal' }],
  departments: [{ id: 'service-delivery', name: 'Service Delivery', key: 'service-delivery' }],
  teams: [{ id: 'security-team', name: 'Security Engineering', key: 'security' }],
  queues: [{ id: 'service-desk', name: 'Service Desk', key: 'service-desk' }],
  technicians: [
    { id: 'marcus-reed', display_name: 'Marcus Reed', name: 'Marcus Reed' },
    { id: 'avery-chen', display_name: 'Avery Chen', name: 'Avery Chen' },
  ],
};
const tickets = [
  ['ticket-1', 'INC-1048', 'incident', 'Remote access unavailable for the delivery team', 'in_progress', 'high', 'marcus-reed', 'Staff cannot connect to the remote access gateway. Initial triage is complete; the network team is reviewing the access policy.'],
  ['ticket-2', 'REQ-1049', 'request', 'Prepare onboarding devices for three new starters', 'new', 'normal', '', 'Provision accounts and prepare managed devices for the next intake.'],
  ['ticket-3', 'CHG-1050', 'change', 'Schedule endpoint hardening rollout', 'in_progress', 'normal', 'avery-chen', 'Coordinate the approved security baseline with the project delivery window.'],
  ['ticket-4', 'INC-1051', 'incident', 'Mailbox synchronization delayed on a shared workstation', 'new', 'low', '', 'Review the cached profile and connection health before the next shift.'],
].map(([ID, DisplayID, Type, Title, Status, Priority, PrimaryOwnerID, Description], index) => ({
  ID, DisplayID, Type, Title, Status, Priority, PrimaryOwnerID, Description,
  QueueID: 'service-desk', UpdatedAt: `${date}T${String(12 - index).padStart(2, '0')}:00:00Z`, Version: 1,
}));
const project = {
  id: 'project-1', display_id: 'PRJ-204', name: 'Security modernization',
  client_name: 'Northwind Legal', lifecycle_state: 'active',
  planned_start: date, planned_end: date, original_proposal_version: 2, version: 3,
  original_baseline: { currency: 'USD', revenue_minor: 4800000, cost_minor: 2400000, planned_minutes: 7200 },
  current_baseline: { currency: 'USD', revenue_minor: 4800000, cost_minor: 2400000, planned_minutes: 7200 },
  phases: [
    { id: 'phase-1', position: 1, name: 'Discovery', state: 'complete', owner_id: 'Marcus Reed', participating_teams: ['Advisory'], planned_minutes: 1200, actual_minutes: 1080, deliverables: ['Approved security architecture'], completion_criteria: ['Client sign-off recorded'], tasks: [], version: 1 },
    { id: 'phase-2', position: 2, name: 'Delivery', state: 'in_progress', owner_id: 'Avery Chen', participating_teams: ['Security Engineering'], planned_minutes: 6000, actual_minutes: 1560, deliverables: ['Hardened endpoint baseline'], completion_criteria: ['Validation report accepted'], tasks: [{ id: 'task-2', title: 'Deploy endpoint hardening baseline', status: 'open', owner_name: 'Avery Chen', subtasks: 4, estimate_minutes: 480, actual_minutes: 120 }], version: 2 },
  ],
  project_tasks: [{ id: 'task-1', title: 'Coordinate pilot cutover', status: 'open', owner_name: 'Marcus Reed', subtasks: 2, estimate_minutes: 240, actual_minutes: 60, version: 1 }],
  resource_plans: [], cost_actuals: [{ id: 'cost-1', cost_type: 'equipment', description: 'Pilot endpoint equipment', amount: money(420000), committed: false, incurred_at: `${date}T09:00:00Z` }],
  financials_visible: true,
  financials: { original_budget: money(4800000), current_budget: money(4800000), planned_labor: money(2400000), actual_labor: money(960000), cost_actuals: money(420000), committed_cost: money(260000), billable_work: money(2200000), profit: money(820000), projected_profit: money(1720000), margin_basis_points: 3583, actual_labor_complete: true, profit_available: true },
  capacity: [
    { id: 'marcus-reed', name: 'Marcus Reed', available_minutes: 2400, scheduled_minutes: 3000, actual_minutes: 600, remaining_minutes: 0, overbooked_minutes: 1200 },
    { id: 'avery-chen', name: 'Avery Chen', available_minutes: 4800, scheduled_minutes: 2400, actual_minutes: 720, remaining_minutes: 2400, overbooked_minutes: 0 },
  ],
  change_orders: [],
};
const events = [
  ['visit', 'Northwind on-site visit', '09', '10', 'scheduled_work', 'work_record', 'ticket-1', 'marcus-reed'],
  ['cutover', 'Endpoint pilot cutover', '11', '12', 'scheduled_work', 'project', 'project-1', 'avery-chen'],
  ['review', 'Security validation review', '14', '15', 'scheduled_work', 'project', 'project-1', 'marcus-reed'],
].map(([id, title, start, end, event_role, type, sourceID, assignee_id]) => ({
  id, title, projection_id: `${id}-projection`, occurrence_id: `${id}-occurrence`, occurrence_key: 'local-occurrence', source_revision: 1,
  starts_at: `${date}T${start}:00:00Z`, ends_at: `${date}T${end}:00:00Z`, all_day: false, timezone: 'UTC',
  event_role, privacy: 'full', source: { type, id: sourceID, client_id: 'client-id' }, assignee_id,
  health: 'on_track', scheduling_mode: 'fixed_block', capacity_bearing: true, capabilities: { view_source: true, schedule: false },
}));
events.push({ id: 'private', projection_id: 'private-projection', occurrence_id: 'private-occurrence', occurrence_key: 'local-occurrence', source_revision: 1, title: 'Busy', privacy: 'busy', all_day: false, starts_at: `${date}T16:00:00Z`, ends_at: `${date}T17:00:00Z`, timezone: 'UTC', capabilities: { view_source: false, schedule: false } });

function fixture(url) {
  const path = url.pathname;
  if (path === '/v1/system/build') return { revision: 'synthetic-preview' };
  if (path === '/api/v1/setup/status') return { completed: true, bootstrap_available: false, entra_available: false };
  if (path === '/api/v1/me') return { id: 'documentation-preview', navigation: ['home', 'work', 'project', 'calendar'], capabilities: ['calendar.read'] };
  if (path === '/api/v1/directory') return directory;
  if (path === '/api/v1/work-records') return tickets.filter((ticket) => {
    const text = (url.searchParams.get('text') || '').toLowerCase();
    const status = url.searchParams.get('status');
    const priority = url.searchParams.get('priority');
    return `${ticket.Title} ${ticket.DisplayID}`.toLowerCase().includes(text) && (!status || ticket.Status === status) && (!priority || ticket.Priority === priority);
  });
  if (path.startsWith('/api/v1/work-records/')) {
    const id = path.split('/')[4];
    if (path.split('/').length === 5) return tickets.find((ticket) => ticket.ID === id);
    return [];
  }
  if (path === '/api/v1/projects') return [project];
  if (path === '/api/v1/projects/project-1') return project;
  if (path === '/api/v1/views') return [];
  if (path === '/api/v1/calendar/events') return { events };
  if (path === '/api/v1/calendar/filter-options') return { clients: { 'client-id': 4 }, technicians: { 'marcus-reed': 2, 'avery-chen': 1 } };
  if (path === '/api/v1/calendar/capacity') return {};
  if (path === '/api/v1/calendar/dependencies') return [];
  if (path === '/api/v1/notifications/unread-count') return { count: 0 };
  if (path === '/api/v1/notifications') return { notifications: [] };
  if (path === '/api/v1/mentions') return { items: [] };
  if (path === '/api/v1/mentions/unread-count') return { count: 0 };
  if (path === '/api/v1/integrations/health') return [];
  if (path === '/api/v1/time-entries/approvals' || path === '/api/v1/admin/audit') return [];
  if (path === '/api/v1/tag-groups') return [{ id: 'technology', label: 'Technology', description: '', state: 'active', version: 1 }];
  if (path === '/api/v1/tags') return [tag];
  if (path === '/api/v1/labor-roles') return [];
  if (path.startsWith('/api/v1/objects/') && path.endsWith('/tags')) {
    const [, , , , object_type, object_id] = path.split('/');
    return { target: { object_type, object_id }, object_version: 1, direct: [tagAssignment], inherited: [], effective: [tagAssignment], classification_state: 'classified' };
  }
  if (path.endsWith('/internal-content')) return [];
}

const server = createServer((request, response) => {
  response.setHeader('Cache-Control', 'no-store');
  response.setHeader('Content-Type', 'application/json');
  // Satisfy the UI's synthetic session heartbeat without creating any session.
  if (request.method === 'POST' && request.url === '/auth/session/refresh') {
    response.writeHead(204);
    return response.end();
  }
  // Fail closed: the preview never performs writes or forwards any request.
  if (request.method !== 'GET') {
    response.writeHead(405, { Allow: 'GET' });
    return response.end(JSON.stringify({ error: { code: 'read_only_documentation_preview' } }));
  }
  const url = new URL(request.url, 'http://127.0.0.1');
  if (url.pathname === '/api/v1/calendar/live') {
    response.writeHead(200, { 'Content-Type': 'text/event-stream' });
    return response.end('event: cursor\ndata: {"cursor":"synthetic-preview"}\n\n');
  }
  const body = fixture(url);
  if (body === undefined) {
    console.warn(`Unsupported preview route: ${url.pathname}`);
    response.writeHead(404);
    return response.end(JSON.stringify({ error: { code: 'preview_route_unavailable' } }));
  }
  response.end(JSON.stringify(body));
});
let vite;
server.on('error', (error) => { console.error(error.message); process.exitCode = 1; });
server.listen(apiPort, '127.0.0.1', async () => {
  console.log(`Synthetic, read-only UI preview: http://127.0.0.1:${uiPort}/#/work`);
  console.log('No database, login credentials, customer data, or external providers are used.');
  try {
    const target = `http://127.0.0.1:${apiPort}`;
    vite = await createViteServer({
      root: `${root}frontend`, configFile: `${root}frontend/vite.config.ts`,
      server: { host: '127.0.0.1', port: uiPort, strictPort: true, proxy: { '/api': target, '/v1': target, '/auth': target } },
    });
    await vite.listen();
    vite.printUrls();
  } catch (error) {
    console.error(error.message);
    await vite?.close();
    server.close();
    process.exitCode = 1;
  }
});
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.once(signal, async () => { await vite?.close(); server.close(); });
}
