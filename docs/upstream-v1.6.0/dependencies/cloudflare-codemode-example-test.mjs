export default {
	async test(_controller, env) {
		const response = await env.AGENT.fetch("https://agent.invalid/", { method: "POST", body: env.SCRIPT });
		const result = await response.json();
		if (response.status !== 200 || result.ok !== true || result.value !== 2 ||
			JSON.stringify(result.output) !== '[{"type":"text","text":"n=2"}]' ||
			result.calls?.length !== 1 || result.calls[0].name !== "count" || result.calls[0].status !== "ok") {
			throw Error(`Unexpected example response: ${response.status} ${JSON.stringify(result)}`);
		}
		const module = await env.MODULE.inspect();
		console.log("CODEMODE_WITNESS", JSON.stringify({
			status: response.status, value: result.value, output: result.output,
			calls: result.calls.map(({ name, status }) => ({ name, status })), module,
		}));
	},
};
