(plan => async ({tools, text, exit}) => {
	const decodeNative = value => {
		if (typeof value !== "string") return value;
		try { return JSON.parse(value); } catch { return null; }
	};
	let current;
	try {
		current = decodeNative(await tools.codex_app__read_thread({
			threadId: plan.task_id,
			includeOutputs: false,
			turnLimit: 1,
			maxOutputCharsPerItem: 1,
		}));
	} catch (error) {
		text(JSON.stringify({ready: false, reason: "Codex title read failed", error: String(error)}));
		exit();
	}
	if (!current || current.thread?.id !== plan.task_id || typeof current.thread.title !== "string") {
		text(JSON.stringify({ready: false, reason: "Codex title read was not confirmed exactly"}));
		exit();
	}
	const previous = current.thread.title;
	if (plan.blocked_prefixes.some(prefix => previous.startsWith(prefix))) {
		text(JSON.stringify({ready: false, reason: "The current title has an ambiguous old ThreadBear prefix"}));
		exit();
	}
	let subject = previous;
	for (const prefix of plan.owned_prefixes) {
		if (subject.startsWith(prefix)) {
			subject = subject.slice(prefix.length);
			break;
		}
	}
	const lower = subject.toLowerCase();
	if (subject.trim() === "" || /[\u0000-\u001f\u007f-\u009f\u2028\u2029]/u.test(subject) ||
		plan.internal_markers.some(marker => lower.includes(marker)) ||
		(plan.icon + " " + subject).length > plan.max_title_units) {
		text(JSON.stringify({ready: false, reason: "The current title is not safe to decorate"}));
		exit();
	}
	const desired = plan.icon + " " + subject;
	if (desired === previous) {
		text(JSON.stringify({ready: true, task_id: plan.task_id, title: previous, updated: false}));
		exit();
	}
	let renamed;
	try {
		renamed = decodeNative(await tools.codex_app__set_thread_title({title: desired}));
	} catch (error) {
		text(JSON.stringify({ready: false, reason: "Codex title write failed", error: String(error)}));
		exit();
	}
	if (!renamed || typeof renamed !== "object" || renamed.threadId !== plan.task_id || renamed.title !== desired) {
		text(JSON.stringify({ready: false, reason: "Codex title write was not confirmed exactly"}));
		exit();
	}
	text(JSON.stringify({ready: true, task_id: plan.task_id, title: renamed.title, updated: true}));
})
