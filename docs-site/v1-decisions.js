const questions = [
  {id:"INF-01",category:"Infrastructure",title:"Which PostgreSQL HA manager and connection router will be supported?",why:"The agreed three-node topology needs concrete failover, health, and connection behavior.",recommendation:"Select one supported HA manager/router pair and publish supported versions plus fencing rules."},
  {id:"INF-02",category:"Infrastructure",title:"What fencing and failure-domain rules prevent split brain?",why:"Automatic failover is only safe when a former primary cannot continue accepting writes.",recommendation:"Require fencing before rejoin and place the three nodes across independent failure domains."},
  {id:"INF-03",category:"Infrastructure",title:"What exact snapshot and WAL cadence proves the five-minute DR loss target?",why:"The recovery target needs measurable schedules, monitoring, and capacity.",recommendation:"Continuous WAL archiving plus scheduled encrypted snapshots; alert before the five-minute recovery-point budget is exceeded."},
  {id:"INF-04",category:"Infrastructure",title:"Which temporary cache and queue technology will V1 support?",why:"Dispatch, rate limits, locks, and presence need a supported operational contract.",recommendation:"Choose one technology; document HA, eviction, persistence limits, encryption, and backlog signals. Keep all business state in PostgreSQL."},
  {id:"INF-05",category:"Infrastructure",title:"Which self-hosted S3-compatible and managed object-storage providers are in the support matrix?",why:"Installers need a clear compatibility promise for attachments and backup artifacts.",recommendation:"Start with one self-hosted S3-compatible option and one major managed S3-compatible service, with validation checks."},
  {id:"INF-06",category:"Infrastructure",title:"What are the V1 capacity and latency budgets?",why:"The scale target of 50 technicians, 1,000 clients, and 5,000 new tickets/day needs testable limits.",recommendation:"Set p95 targets for interactive reads, mutations, search, intake acknowledgment, attachment upload/download, and maximum storage growth."},
  {id:"INF-07",category:"Infrastructure",title:"What is the supported single-node hardware and maintenance profile?",why:"Docker-first deployments need a credible non-HA path as well as HA.",recommendation:"Publish minimum CPU, memory, storage, backup, and guided maintenance-window requirements."},
  {id:"INF-08",category:"Infrastructure",title:"What are the precise upgrade compatibility and rollback rules?",why:"Rolling HA upgrades and single-node maintenance need predictable operator behavior.",recommendation:"Use expand/migrate/contract database changes, mandatory pre-upgrade backup, health checks, and explicit irreversible-migration warnings."},

  {id:"SEC-01",category:"Security",title:"Which encryption/key-management providers are supported in V1?",why:"Encryption is mandatory, but operational recovery depends on an implementable key hierarchy.",recommendation:"Define a default local deployment model and a supported external KMS/secret-provider path; document rotation and recovery."},
  {id:"SEC-02",category:"Security",title:"What are the break-glass account controls and exercise schedule?",why:"Local emergency administrators are necessary during Entra outage but introduce elevated risk.",recommendation:"Restrict use, require strong MFA where feasible, alert on every use, and test quarterly alongside DR."},
  {id:"SEC-03",category:"Security",title:"What session idle timeout, absolute timeout, and revocation behavior apply?",why:"Technicians need usable sessions while MSP data needs protection on shared or lost devices.",recommendation:"Set role-sensitive defaults and make active-session revocation visible to administrators."},
  {id:"SEC-04",category:"Security",title:"Which actions require an explicit destructive confirmation in V1?",why:"V1 does not require step-up MFA or dual approval, so confirmation behavior must be consistent.",recommendation:"Include permanent erasure, restore execution, broad export, connection disablement, secret rotation, and role changes."},
  {id:"SEC-05",category:"Security",title:"What log, audit, and support-bundle access policy applies?",why:"Operational diagnosis must not become a cross-client data leak.",recommendation:"Define role-scoped access, redaction rules, retention, and audited export requirements."},

  {id:"MAIL-01",category:"Intake and API",title:"Which Microsoft Graph mailbox permissions and mailbox topology are supported?",why:"Graph notifications trigger secure retrieval, but the supported tenant/mailbox model must be explicit.",recommendation:"Start with one dedicated shared support mailbox and least-privilege application permissions."},
  {id:"MAIL-02",category:"Intake and API",title:"What are the Graph subscription renewal, retry, and outage rules?",why:"Subscriptions expire and delivery can fail; missed mail must be detected and recovered.",recommendation:"Renew ahead of expiry, periodically reconcile mailbox state, and alert on subscription or retrieval failure."},
  {id:"MAIL-03",category:"Intake and API",title:"What forwarding intake domain, sender validation, and anti-spoofing policy will Rarity use?",why:"Forwarded email needs predictable routing without accepting arbitrary forged messages.",recommendation:"Use a dedicated intake domain, documented forwarding setup, sender/forwarding validation, bounded payloads, and quarantine visibility."},
  {id:"MAIL-04",category:"Intake and API",title:"What exact threading fallback order should create/update a ticket?",why:"Message IDs are ideal, but forwarding and client behavior are imperfect.",recommendation:"Prefer ticket token, then message/reply identifiers, then carefully bounded subject/contact/time correlation; show ambiguity for review."},
  {id:"API-01",category:"Intake and API",title:"What service API-key scopes and expiration defaults should ship?",why:"Integrations must be least-privileged and manageable.",recommendation:"Scope by client/object/action; default to expiration and rotation reminders; never allow wildcard client access without explicit administrator intent."},
  {id:"API-02",category:"Intake and API",title:"What personal-access-token lifetime and allowed scopes are appropriate for technician scripts?",why:"PATs are convenient but high-risk if long-lived or broad.",recommendation:"Use short default expiry, explicit scopes, last-used visibility, and revocation; forbid administrative/security scopes initially."},
  {id:"API-03",category:"Intake and API",title:"What webhook retry schedule and disablement threshold should be standard?",why:"Outbound events need reliable delivery without endlessly calling a broken target.",recommendation:"Use exponential backoff, bounded attempts, visible failures, dead-letter handling, and automatic disablement with administrator notification."},
  {id:"API-04",category:"Intake and API",title:"Which API rate limits, payload limits, and idempotency retention periods are V1 defaults?",why:"The public API needs predictable fairness and safety under integration load.",recommendation:"Set per-principal and per-connection limits, document override policy, and retain idempotency results long enough for normal client retries."},

  {id:"DAT-01",category:"Datto RMM",title:"Which Datto API version and authentication model will V1 support?",why:"The first full RMM integration needs a concrete, maintainable contract.",recommendation:"Support one documented Datto API generation and one least-privilege credential model; validate it in the setup wizard."},
  {id:"DAT-02",category:"Datto RMM",title:"What is the default incremental sync interval and allowed range?",why:"A 15-minute suggestion needs capacity, rate-limit, and freshness expectations.",recommendation:"Default to 15 minutes, allow a bounded administrator-configurable range, and show last-success and lag."},
  {id:"DAT-03",category:"Datto RMM",title:"Which fields drive candidate matching and what confidence threshold permits automatic linking?",why:"Reconciliation must be safe and explainable.",recommendation:"Use stable IDs/serials first, then carefully weighted hostname/client/location; automatic linking only above a high threshold and always auditable."},
  {id:"DAT-04",category:"Datto RMM",title:"Which fields let Datto win automatically, if any?",why:"“Choose Rarity” versus “choose Datto” needs field-level rather than vague behavior.",recommendation:"Default to Rarity for technician-curated fields; allow Datto to update clearly discovered telemetry with provenance."},
  {id:"DAT-05",category:"Datto RMM",title:"What alert deduplication fingerprint and time window are the defaults?",why:"Repeated alerts should update the same open incident without hiding distinct failures.",recommendation:"Use client + source device/service + alert type + normalized condition; default window should be configurable per alert policy."},
  {id:"DAT-06",category:"Datto RMM",title:"When does a missing Datto asset become stale versus inactive?",why:"Assets must not be deleted automatically, but technicians need useful lifecycle signals.",recommendation:"Use configurable consecutive-miss or elapsed-time thresholds with review queue and audit history."},
  {id:"DAT-07",category:"Datto RMM",title:"What sync errors retry automatically, and when is human review required?",why:"Rate limits, credentials, schema changes, and inconsistent data need distinct handling.",recommendation:"Retry transient failures with backoff; require review for authorization, schema, destructive mapping, and persistent reconciliation failures."},

  {id:"OPS-01",category:"Technician experience",title:"Which default SLA warning thresholds and escalation recipients should ship?",why:"Warning/breach alerts are configurable, but useful defaults reduce setup time.",recommendation:"Provide percentage/time-based defaults by priority and route to owner, queue manager, then service manager."},
  {id:"OPS-02",category:"Technician experience",title:"What is the standard set of public-reply templates and support-mailbox signatures?",why:"Clear, consistent client communication is a V1 differentiator.",recommendation:"Ship minimal acknowledge, update, scheduled work, resolution, and closure templates with MSP/queue signature variables."},
  {id:"OPS-03",category:"Technician experience",title:"What Teams delivery/authentication path will V1 support?",why:"Teams is a committed channel and needs a secure, supportable integration path.",recommendation:"Select one supported Microsoft-native delivery model with least privilege, per-connection health, and actionable delivery errors."},
  {id:"OPS-04",category:"Technician experience",title:"Which worklist columns, filters, and saved views are provided by default?",why:"Technicians need effective defaults before customization.",recommendation:"Include ID, title, client, type, priority, state, owner, queue, SLA risk, scheduled start, updated time, and source."},
  {id:"OPS-05",category:"Technician experience",title:"What events create in-app notifications, and which are digestible versus immediate?",why:"Too many notifications undermine triage; too few hide SLA risk.",recommendation:"Make assignment, mentions, public replies, SLA warning/breach, scheduled reminders, and integration failures immediate; make low-risk activity digestible."},
  {id:"OPS-06",category:"Technician experience",title:"What fields are required to transition to Resolved, Closed, or Cancelled?",why:"Closure quality and audit expectations need an explicit baseline.",recommendation:"Require resolution for Resolved, closure reason where applicable, and workflow-controlled validation; keep MSP overrides auditable."},
  {id:"OPS-07",category:"Technician experience",title:"What duplicate-merge eligibility and redirect behavior should be enforced?",why:"Merging preserves history but can confuse integrations and client communication.",recommendation:"Allow same-client compatible records only; preserve source history, create durable redirects, block duplicate external updates safely, and never notify requesters automatically."},
  {id:"OPS-08",category:"Technician experience",title:"Which attachment formats count as safe inline previews?",why:"V1 deliberately has no scanner, so display policy must remain conservative.",recommendation:"Allow common raster screenshots only after authorization; make documents, archives, executables, scripts, and active content download-only."},
  {id:"OPS-09",category:"Technician experience",title:"What dashboard sharing defaults and guardrails apply?",why:"Dashboards are shareable but must not leak cross-client data.",recommendation:"Share by named scope only; apply current permission checks at view time; prohibit embedding/exporting broader data than the viewer may access."},
  {id:"OPS-10",category:"Technician experience",title:"What calendar and timezone behavior should scheduled work use?",why:"Scheduled work spans technician, client, and contract calendars.",recommendation:"Store UTC, display viewer timezone by default, show client timezone where relevant, and make calendar/timezone context explicit."},

  {id:"DEL-01",category:"Delivery and governance",title:"Which CI system and artifact registry will the project standardize on?",why:"Phase 0 needs a reproducible delivery baseline before code starts.",recommendation:"Choose one CI provider and one artifact registry with signed artifacts, SBOMs, scans, and protected release paths."},
  {id:"DEL-02",category:"Delivery and governance",title:"Which dependency, secret, license, and container scanning tools are required?",why:"Security checks must be consistent and actionable.",recommendation:"Use a minimal consolidated toolchain with blocking severity policy and documented exception process."},
  {id:"DEL-03",category:"Delivery and governance",title:"What acceptance-environment lifecycle and synthetic-data reset policy will be used?",why:"Tests need repeatable fixtures without retaining customer data.",recommendation:"Create/reseed synthetic environments predictably; retain failure artifacts only as long as needed for diagnosis."},
  {id:"DEL-04",category:"Delivery and governance",title:"What release cadence and emergency-patch policy should V1 follow?",why:"Upgrade and rollback commitments need an operational rhythm.",recommendation:"Use regular planned releases, documented maintenance notices for single-node customers, and a fast tracked security-patch path."}
];

const storeKey = "rarity-v1-decision-workbook-v1";
const projectDefaults = new Set(questions.map(question => question.id));
const load = () => { try { return JSON.parse(localStorage.getItem(storeKey)) || {}; } catch { return {}; } };
let answers = load();
const save = () => localStorage.setItem(storeKey, JSON.stringify(answers));
const questionSpace = document.querySelector("#questions");
const search = document.querySelector("#question-search");
const category = document.querySelector("#question-category");
const openOnly = document.querySelector("#open-only");
const noQuestions = document.querySelector("#no-questions");

[...new Set(questions.map(q => q.category))].forEach(name => {
  const option = document.createElement("option"); option.value = name; option.textContent = name; category.append(option);
});

function updateProgress() {
  const completed = questions.filter(q => projectDefaults.has(q.id) || answers[q.id]?.done).length;
  document.querySelector("#complete-count").textContent = completed;
  document.querySelector("#total-count").textContent = questions.length;
  const percentage = Math.round((completed / questions.length) * 100);
  document.querySelector("#progress-fill").style.width = `${percentage}%`;
  document.querySelector("#progress-text").textContent = completed === questions.length
    ? "Every decision is marked complete. Export the worksheet and send it back for documentation updates."
    : `${percentage}% complete. Add notes or exceptions to any approved baseline as needed.`;
}

function render() {
  const term = search.value.trim().toLowerCase();
  const selected = category.value;
  const visible = questions.filter(q => {
    const response = answers[q.id]?.text || "";
    return (selected === "all" || q.category === selected) &&
      (!openOnly.checked || !(projectDefaults.has(q.id) || answers[q.id]?.done)) &&
      (!term || `${q.id} ${q.category} ${q.title} ${q.why} ${q.recommendation} ${response}`.toLowerCase().includes(term));
  });
  questionSpace.replaceChildren();
  for (const q of visible) {
    const entry = answers[q.id] || {};
    const article = document.createElement("article");
    const approved = projectDefaults.has(q.id);
    const decided = approved || entry.done;
    article.className = `decision-question${decided ? " is-done" : ""}`;
    article.innerHTML = `
      <div class="question-heading"><span class="question-id">${q.id}</span><span class="question-category">${q.category}</span></div>
      <h2>${q.title}</h2>
      <p><strong>Why it matters:</strong> ${q.why}</p>
      <p class="recommendation"><strong>Recommended default:</strong> ${q.recommendation}</p>
      <label class="answer-label">${approved ? "Notes or exception" : "Your decision or constraint"}<textarea data-answer="${q.id}" rows="4" placeholder="${approved ? "Project default is approved. Add an exception or implementation note if needed…" : "Write your answer, approved default, constraints, or follow-up…"}"></textarea></label>
      <label class="decision-toggle"><input data-done="${q.id}" type="checkbox" ${approved ? "disabled" : ""}> ${approved ? "Project default approved" : "Mark decided"}</label>
    `;
    article.querySelector("textarea").value = entry.text || "";
    article.querySelector("input").checked = decided;
    questionSpace.append(article);
  }
  noQuestions.hidden = visible.length !== 0;
  updateProgress();
}

questionSpace.addEventListener("input", event => {
  if (!event.target.matches("textarea[data-answer]")) return;
  const id = event.target.dataset.answer; answers[id] = {...answers[id], text: event.target.value}; save();
});
questionSpace.addEventListener("change", event => {
  if (!event.target.matches("input[data-done]")) return;
  const id = event.target.dataset.done; answers[id] = {...answers[id], done: event.target.checked}; save(); render();
});
[search, category, openOnly].forEach(control => control.addEventListener("input", render));

document.querySelector("#export-button").addEventListener("click", () => {
  const date = new Date().toISOString().slice(0, 10);
  const groups = [...new Set(questions.map(q => q.category))];
  const lines = ["# Rarity V1 Technician Platform Decisions", "", `Exported: ${date}`, ""];
  for (const group of groups) {
    lines.push(`## ${group}`, "");
    for (const q of questions.filter(item => item.category === group)) {
      const approved = projectDefaults.has(q.id);
      const answer = answers[q.id]?.text?.trim() || (approved ? "Approved recommended default" : "Not answered");
      const state = approved || answers[q.id]?.done ? "Decided" : "Open";
      lines.push(`### ${q.id}: ${q.title}`, "", `**Status:** ${state}`, "", `**Decision:** ${answer}`, "", `**Recommended default:** ${q.recommendation}`, "");
    }
  }
  const link = document.createElement("a");
  link.href = URL.createObjectURL(new Blob([lines.join("\n")], {type:"text/markdown"}));
  link.download = `rarity-v1-decisions-${date}.md`; link.click(); URL.revokeObjectURL(link.href);
});

document.querySelector("#clear-button").addEventListener("click", () => {
  if (!confirm("Clear all locally saved V1 decision answers in this browser? Export first if you want to keep them.")) return;
  answers = {}; save(); render();
});

render();
