import { execFile } from "node:child_process";
import { readFile, readdir } from "node:fs/promises";
import { promisify } from "node:util";
import { dirname, extname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import * as prettier from "../frontend/node_modules/prettier/index.mjs";
import typescriptPlugin from "../frontend/node_modules/prettier/plugins/typescript.mjs";

const executeFile = promisify(execFile);
const sourceExtensions = new Set([".go", ".sql", ".ts", ".tsx", ".js", ".mjs"]);
const preservationManifest = {
  forecast_category: [
    { kind: "go", path: "backend/internal/sales/model.go", type: "PipelineStage", field: "Category", fieldType: "ForecastCategory", json: "forecast_category" },
    { kind: "sql", path: "backend/migrations/000010_psa_sales.sql", table: "pipeline_stages", column: "forecast_category" },
  ],
  content_classification: [
    { kind: "go", path: "backend/internal/notifications/management.go", type: "Destination", field: "ContentClassification", fieldType: "string", json: "content_classification" },
    { kind: "sql", path: "backend/migrations/000005_workflow_experience.sql", table: "notification_deliveries", column: "content_classification" },
  ],
  ai_context_classification: [
    { kind: "go", path: "backend/internal/aiassist/service.go", type: "ContextClassification", field: "", fieldType: "string", json: "" },
    { kind: "go", path: "backend/internal/aiassist/service.go", type: "ContextField", field: "Classification", fieldType: "ContextClassification", json: "" },
  ],
  billing_classification: [
    { kind: "go", path: "backend/internal/billingexport/export.go", type: "Entry", field: "Billable", fieldType: "bool", json: "billable" },
    { kind: "sql", path: "backend/migrations/000004_work_management.sql", table: "time_entries", column: "billable" },
  ],
};

async function sourcePaths(directory) {
  let entries;
  try {
    entries = await readdir(directory, { withFileTypes: true });
  } catch {
    return [];
  }
  const nested = await Promise.all(entries
    .filter((entry) => !["node_modules", "dist", ".git"].includes(entry.name))
    .map(async (entry) => {
      const path = resolve(directory, entry.name);
      if (entry.isDirectory()) return sourcePaths(path);
      return entry.isFile() && sourceExtensions.has(extname(entry.name)) ? [path] : [];
    }));
  return nested.flat();
}

function lineFor(source, offset) {
  return source.slice(0, offset).split("\n").length;
}

function propertyName(node) {
  if (!node) return "";
  if (node.type === "Identifier") return node.name;
  if (node.type === "Literal" || node.type === "StringLiteral" || node.type === "NumericLiteral") return String(node.value);
  return "";
}

function hasCategoryPath(value) {
  return value.split(/[/?&=]/u).some((part) => part.toLowerCase() === "category");
}

async function inspectTypeScript(path, source) {
  const violations = [];
  const { ast } = await prettier.__debug.parse(source, {
    parser: [".tsx", ".jsx"].includes(extname(path)) ? "typescript" : "typescript",
    plugins: [typescriptPlugin],
  });
  function visit(node) {
    if (!node || typeof node !== "object") return;
    const isProperty = ["TSPropertySignature", "Property", "PropertyDefinition", "ObjectProperty"].includes(node.type);
    if (isProperty && propertyName(node.key).toLowerCase() === "category") {
      violations.push({ path, line: node.loc?.start?.line ?? 1,
        message: "generic Category property remains in a TypeScript contract or schema" });
    }
    const literal = node.type === "Literal" && typeof node.value === "string" ? node.value :
      node.type === "StringLiteral" ? node.value : "";
    if (literal && hasCategoryPath(literal)) {
      violations.push({ path, line: node.loc?.start?.line ?? 1,
        message: "generic Category API path remains" });
    }
    for (const [key, child] of Object.entries(node)) {
      if (["loc", "range", "comments", "tokens", "parent"].includes(key)) continue;
      if (Array.isArray(child)) child.forEach(visit);
      else visit(child);
    }
  }
  visit(ast);
  return violations;
}

function stripSQLCommentsAndStrings(source) {
  return source
    .replace(/--[^\n]*/gu, (value) => " ".repeat(value.length))
    .replace(/\/\*[\s\S]*?\*\//gu, (value) => value.replace(/[^\n]/gu, " "))
    .replace(/'(?:''|[^'])*'/gu, (value) => value.replace(/[^\n]/gu, " "));
}

function inspectSQL(path, source) {
  const structural = stripSQLCommentsAndStrings(source);
  const patterns = [
    /(?:\(|,)\s*category\s+[a-z_][a-z0-9_]*/giu,
    /\badd\s+(?:column\s+)?category\b/giu,
    /\brename\s+(?:column\s+)?[a-z_][a-z0-9_]*\s+to\s+category\b/giu,
  ];
  const offsets = new Set(patterns.flatMap((pattern) => [...structural.matchAll(pattern)].map((match) =>
    (match.index ?? 0) + match[0].toLowerCase().lastIndexOf("category"))));
  return [...offsets].map((offset) => ({ path, line: lineFor(structural, offset), message: "generic Category SQL column remains" }));
}

async function inspectGo(paths, root) {
  if (!paths.length) return { violations: [], contracts: [] };
  const helper = resolve(import.meta.dirname, "validate-classification-cutover-go/main.go");
  const { stdout } = await executeFile("go", ["run", helper, "--", ...paths], { maxBuffer: 16 * 1024 * 1024 });
  const result = JSON.parse(stdout);
  return {
    violations: result.violations.map((violation) => ({ ...violation, path: relative(root, violation.path).replaceAll("\\", "/") })),
    contracts: result.contracts.map((contract) => ({ ...contract, path: relative(root, contract.path).replaceAll("\\", "/") })),
  };
}

function sqlContractPresent(source, requirement) {
  const structural = stripSQLCommentsAndStrings(source);
  const table = requirement.table.replaceAll("_", "[_]");
  const match = structural.match(new RegExp(`\\bcreate\\s+table\\s+${table}\\s*\\(([\\s\\S]*?)\\);`, "iu"));
  return Boolean(match && new RegExp(`(?:^|,)\\s*${requirement.column}\\s+[a-z_]`, "imu").test(match[1]));
}

async function missingProductionPreservations(repositoryRoot, goContracts) {
  const missing = [];
  for (const [name, requirements] of Object.entries(preservationManifest)) {
    let complete = true;
    for (const requirement of requirements) {
      if (requirement.kind === "go") {
        complete &&= goContracts.some((contract) =>
          contract.path === requirement.path && contract.type === requirement.type &&
          contract.field === requirement.field && contract.field_type === requirement.fieldType &&
          contract.json === requirement.json);
      } else {
        let source = "";
        try { source = await readFile(resolve(repositoryRoot, requirement.path), "utf8"); } catch {}
        complete &&= sqlContractPresent(source, requirement);
      }
    }
    if (!complete) missing.push(name);
  }
  return missing;
}

export async function validateClassificationCutover(root) {
  const repositoryRoot = resolve(root);
  const files = (await Promise.all(["backend", "frontend"].map((directory) => sourcePaths(resolve(repositoryRoot, directory))))).flat();
  const goFiles = files.filter((path) => extname(path) === ".go");
  const inspectedGo = await inspectGo(goFiles, repositoryRoot);
  const violations = inspectedGo.violations;
  for (const path of files.filter((value) => extname(value) !== ".go")) {
    const source = await readFile(path, "utf8");
    const extension = extname(path);
    const found = extension === ".sql" ? inspectSQL(path, source) : await inspectTypeScript(path, source);
    violations.push(...found.map((violation) => ({ ...violation, path: relative(repositoryRoot, path).replaceAll("\\", "/") })));
  }
  violations.sort((left, right) => left.path.localeCompare(right.path) || left.line - right.line);
  const missingPreservations = await missingProductionPreservations(repositoryRoot, inspectedGo.contracts);
  return { ok: violations.length === 0 && missingPreservations.length === 0,
    filesScanned: files.length, violations, missingPreservations };
}

const invokedPath = process.argv[1] ? resolve(process.argv[1]) : "";
if (invokedPath === fileURLToPath(import.meta.url)) {
  const root = process.argv[2] ? resolve(process.argv[2]) : resolve(dirname(invokedPath), "..");
  const result = await validateClassificationCutover(root);
  for (const violation of result.violations) console.error(`${violation.path}:${violation.line}: ${violation.message}`);
  for (const name of result.missingPreservations) console.error(`missing preserved classification contract: ${name}`);
  if (!result.ok) process.exitCode = 1;
  else console.log(`Classification cutover source valid: ${result.filesScanned} files structurally scanned; generic Category absent; forecast_category, content_classification, AI context classification, and billing classification positively verified.`);
}
