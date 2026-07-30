#!/usr/bin/env node
import assert from "node:assert/strict"; import vm from "node:vm";
const RAW_CELL = String.raw`// @exec: {"yield_time_ms": 120000, "max_output_tokens": 1000}
const counts = {accepted: 0, canonically_verified: 0, failed: 0, timed_out: 0, drifted: 0, rejected: 0}; let waitsRemaining = 300; const deadlineAt = Date.now() + 300000; const command = async (cmd) => { const result = await tools.exec_command({cmd}); if (!result || result.exit_code !== 0) throw {kind: "command_failed"}; return result; }; const commandJSON = async (cmd) => { const result = await command(cmd); if (typeof result.output !== "string") throw {kind: "command_failed"}; return JSON.parse(result.output); };
const readyJSON = async (cmd, waitForContinuation = false) => { for (;;) {
  const value = await commandJSON(cmd);
  if (value.ready && (!value.continuation_due || !waitForContinuation)) return value;
  if (!value.ready && (!value.retryable || !["heartbeat_active", "heartbeat_cycle_active"].includes(value.error_code))) throw {kind: "not_ready"};
  if (waitsRemaining-- <= 0 || Date.now() >= deadlineAt) { counts.timed_out++; throw {kind: "timed_out"}; }
  await command("sleep 1");
} };
const quote = (value) => "'" + value.replaceAll("'", "'\\''") + "'";
const batchCommand = "~/.local/bin/threadbear title-plan --json --batch", stageFooter = (footer) => readyJSON("printf %s " + quote(footer) + " | ~/.local/bin/threadbear title-plan --json --stage");
const drain = async (batch) => { let complete = true;
  for (const operationID of batch.operation_ids || []) try {
    const operation = await readyJSON("~/.local/bin/threadbear title-plan --json --operation " + quote(operationID));
    if (operation.disposition === "drifted") { counts.drifted++; complete = false; continue; }
    if (operation.disposition !== "ready" || !["set", "report_success"].includes(operation.action)) { counts.rejected++; complete = false; continue; }
    let outcome = "succeeded", errorCode = "";
    if (operation.action === "set") try { await tools.codex_app__set_thread_title({threadId: operation.task_id, title: operation.desired_title}); }
    catch { outcome = "failed"; errorCode = "native_setter_failed"; counts.failed++; complete = false; }
    const payload = {reports: [{operation_id: operationID, outcome, ...(errorCode && {error_code: errorCode})}]};
    const report = await readyJSON("printf %s " + quote(JSON.stringify(payload)) + " | ~/.local/bin/threadbear title-plan --json --report");
    counts.accepted += report.accepted || 0;
    if (outcome === "succeeded") counts.canonically_verified += report.accepted || 0;
  } catch (error) { if (error?.kind !== "timed_out") counts.failed++; complete = false; }
  return complete;
};
let coreComplete = false, complete = false;
try {
  const first = await readyJSON(batchCommand);
  if (await drain(first)) {
    let remaining = first.continuation_due ? first : await readyJSON(batchCommand);
    if (remaining.continuation_due) {
      await command('launchctl kickstart "gui/$(id -u)/org.litman.threadbear"');
      remaining = await readyJSON(batchCommand, true);
    }
    coreComplete = await drain(remaining);
  }
} catch (error) { if (error?.kind !== "timed_out") counts.failed++; }
if (coreComplete) try {
  await stageFooter("🧵🐻 complete");
  complete = await drain(await readyJSON(batchCommand));
} catch (error) { if (error?.kind !== "timed_out") counts.failed++; }
if (!complete) try {
  await stageFooter("🧵🐻 next steps (agent): retry the first title handoff");
  await drain(await readyJSON(batchCommand));
} catch (error) { if (error?.kind !== "timed_out") counts.failed++; }
text(JSON.stringify({...counts, complete}));`;
const runCell = async (scenario) => {
  const output = [], tools = {exec_command: ({cmd}) => scenario.exec(cmd), codex_app__set_thread_title: (payload) => scenario.set(payload)};
  await new vm.Script(`(async()=>{${RAW_CELL}\n})()`).runInContext(vm.createContext({tools, text: (value) => output.push(value)})); assert.equal(output.length, 1); return output[0];
};
const fixture = (options = {}) => {
  const secrets = ["0123456789abcdef0123456789abcdef", "sensitive-task", "✅ Sensitive title"]; let batches = 0, logicalBatches = 0, kickstarts = 0, stages = 0, stagedBatches = 0;
  return {secrets, async exec(cmd) {
    if (cmd === "sleep 1") return {exit_code: 0, output: ""};
    if (cmd.includes("launchctl kickstart")) { kickstarts++; return options.kickstartFailure ? {exit_code: 1, output: ""} : {exit_code: 0, output: ""}; }
    if (cmd.includes("--stage")) { stages++; return {exit_code: 0, output: JSON.stringify({ready: true})}; }
    if (cmd.includes("--batch")) {
      batches++;
      if (stages > stagedBatches) { stagedBatches++; return {exit_code: 0, output: JSON.stringify({ready: true, operation_ids: [secrets[0]]})}; }
      if (options.retry && batches === 1) return {exit_code: 0, output: JSON.stringify({ready: false, retryable: true, error_code: "heartbeat_active"})};
      logicalBatches++;
      if (options.initialContinuationDue && logicalBatches === 1) return {exit_code: 0, output: JSON.stringify({ready: true, continuation_due: true})};
      if (options.initialContinuationDue && logicalBatches === 2) return {exit_code: 0, output: JSON.stringify({ready: true, operation_ids: [secrets[0]]})};
      if (logicalBatches === 1) return {exit_code: 0, output: JSON.stringify({ready: true, operation_ids: [secrets[0]]})};
      if (options.continuationAlreadyComplete && logicalBatches === 2) return {exit_code: 0, output: JSON.stringify({ready: true})};
      if (logicalBatches === 2 || options.timeout && logicalBatches > 2) return {exit_code: 0, output: JSON.stringify({ready: true, continuation_due: true})};
      return {exit_code: 0, output: JSON.stringify({ready: true})};
    }
    if (cmd.includes("--operation")) return {exit_code: 0, output: JSON.stringify(stages === 0 && options.operation || {ready: true, disposition: "ready", action: "set", task_id: secrets[1], desired_title: secrets[2]})};
    return options.reportFailure ? {exit_code: 1, output: ""} : {exit_code: 0, output: JSON.stringify({ready: true, accepted: 1})};
  }, batches: () => batches, kickstarts: () => kickstarts, stages: () => stages, async set({threadId, title}) { assert.deepEqual([threadId, title], secrets.slice(1)); if (options.setterFailure || options.footerSetterFailure && stages === 1) throw new Error(); }};
};
const cases = [[{}, [2,2,0,0,0,0,true]], [{retry:true}, [2,2,0,0,0,0,true]], [{initialContinuationDue:true}, [2,2,0,0,0,0,true]], [{continuationAlreadyComplete:true}, [2,2,0,0,0,0,true]], [{kickstartFailure:true}, [2,2,1,0,0,0,false]], [{operation:{ready:true,disposition:"drifted"}}, [1,1,0,0,1,0,false]], [{operation:{ready:true,disposition:"missing"}}, [1,1,0,0,0,1,false]], [{reportFailure:true}, [0,0,2,0,0,0,false]], [{setterFailure:true}, [2,0,2,0,0,0,false]], [{footerSetterFailure:true}, [3,2,1,0,0,0,false]]]; for (const [options, values] of cases) {
  const scenario = fixture(options), output = await runCell(scenario); assert.deepEqual(Object.values(JSON.parse(output)), values);
  for (const secret of scenario.secrets) assert.equal(output.includes(secret), false);
  assert.equal(scenario.kickstarts(), !options.continuationAlreadyComplete && !options.reportFailure && !options.setterFailure && !options.operation ? 1 : 0);
  assert.equal(scenario.stages(), values.at(-1) ? 1 : options.footerSetterFailure ? 2 : 1);
}
const timed = fixture({timeout: true}), timedOutput = JSON.parse(await runCell(timed)); assert.equal(timedOutput.timed_out, 1); assert.equal(timedOutput.complete, false); assert.equal(timed.kickstarts(), 1); assert.equal(timed.stages(), 1);
assert.equal(/\b(process|setTimeout|functions\.|call\.wait|require\(|console\.)/.test(RAW_CELL), false); console.log("title batch replay harness passed"); export {RAW_CELL};
