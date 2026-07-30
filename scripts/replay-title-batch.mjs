#!/usr/bin/env node
import assert from "node:assert/strict"; import vm from "node:vm";
const RAW_CELL = String.raw`// @exec: {"yield_time_ms": 120000, "max_output_tokens": 1000}
const counts = {accepted: 0, canonically_verified: 0, failed: 0, drifted: 0, rejected: 0}; const commandJSON = async (cmd) => { const result = await tools.exec_command({cmd}); if (!result || result.exit_code !== 0 || typeof result.output !== "string") throw new Error("command_failed"); return JSON.parse(result.output); };
const readyJSON = async (cmd) => { for (;;) {
  const value = await commandJSON(cmd);
  if (value.ready) return value;
  if (!value.retryable || !["heartbeat_active", "heartbeat_cycle_active"].includes(value.error_code)) throw new Error("not_ready");
  if ((await tools.exec_command({cmd: "sleep 1"})).exit_code !== 0) throw new Error("sleep_failed");
} };
const quote = (value) => "'" + value.replaceAll("'", "'\\''") + "'";
try {
  for (const operationID of (await readyJSON("~/.local/bin/threadbear title-plan --json --batch")).operation_ids || []) try {
    const operation = await readyJSON("~/.local/bin/threadbear title-plan --json --operation " + quote(operationID));
    if (operation.disposition === "drifted") { counts.drifted++; continue; }
    if (operation.disposition !== "ready" || !["set", "report_success"].includes(operation.action)) { counts.rejected++; continue; }
    let outcome = "succeeded", errorCode = "";
    if (operation.action === "set") try { await tools.codex_app__set_thread_title({threadId: operation.task_id, title: operation.desired_title}); }
    catch { outcome = "failed"; errorCode = "native_setter_failed"; counts.failed++; }
    const payload = {reports: [{operation_id: operationID, outcome, ...(errorCode && {error_code: errorCode})}]};
    const report = await readyJSON("printf %s " + quote(JSON.stringify(payload)) + " | ~/.local/bin/threadbear title-plan --json --report");
    counts.accepted += report.accepted || 0;
    if (outcome === "succeeded") counts.canonically_verified += report.accepted || 0;
  } catch { counts.failed++; }
} catch { counts.failed++; }
text(JSON.stringify(counts));`;
const runCell = async (scenario) => {
  const output = [], tools = {exec_command: ({cmd}) => scenario.exec(cmd), codex_app__set_thread_title: (payload) => scenario.set(payload)};
  await new vm.Script(`(async()=>{${RAW_CELL}\n})()`).runInContext(vm.createContext({tools, text: (value) => output.push(value)})); assert.equal(output.length, 1); return output[0];
};
const fixture = (options = {}) => {
  const secrets = ["0123456789abcdef0123456789abcdef", "sensitive-task", "✅ Sensitive title"]; let batches = 0;
  return {secrets, async exec(cmd) {
    if (cmd === "sleep 1") return {exit_code: 0, output: ""};
    if (cmd.includes("--batch")) return {exit_code: 0, output: JSON.stringify(options.retry && !batches++ ? {ready: false, retryable: true, error_code: "heartbeat_active"} : {ready: true, operation_ids: [secrets[0]]})};
    if (cmd.includes("--operation")) return {exit_code: 0, output: JSON.stringify(options.operation || {ready: true, disposition: "ready", action: "set", task_id: secrets[1], desired_title: secrets[2]})};
    return options.reportFailure ? {exit_code: 1, output: ""} : {exit_code: 0, output: JSON.stringify({ready: true, accepted: 1})};
  }, async set({threadId, title}) { assert.deepEqual([threadId, title], secrets.slice(1)); if (options.setterFailure) throw new Error(); }};
};
const cases = [[{}, [1,1,0,0,0]], [{retry:true}, [1,1,0,0,0]], [{operation:{ready:true,disposition:"drifted"}}, [0,0,0,1,0]], [{operation:{ready:true,disposition:"missing"}}, [0,0,0,0,1]], [{reportFailure:true}, [0,0,1,0,0]], [{setterFailure:true}, [1,0,1,0,0]]]; for (const [options, values] of cases) {
  const scenario = fixture(options), output = await runCell(scenario); assert.deepEqual(Object.values(JSON.parse(output)), values);
  for (const secret of scenario.secrets) assert.equal(output.includes(secret), false);
}
assert.equal(/\b(process|setTimeout|functions\.|call\.wait|require\(|console\.)/.test(RAW_CELL), false); console.log("title batch replay harness passed"); export {RAW_CELL};
