import { ref } from "vue";
import { useMCP } from "./useMCP";

export function useStream() {
	const { callTool } = useMCP();
	const streaming = ref(false);
	const error = ref<string | null>(null);

	async function streamAgent(
		agentName: string,
		message: string,
		onChunk: (text: string) => void,
		session = "main",
	): Promise<void> {
		streaming.value = true;
		error.value = null;

		try {
			let sawProgress = false;
			const text = await callTool(
				"agent_run",
				{
					name: agentName,
					message,
					session,
				},
				{
					onProgress: (chunk) => {
						sawProgress = true;
						onChunk(chunk);
					},
				},
			);
			if (!sawProgress && text) {
				onChunk(text);
			}
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
			throw e;
		} finally {
			streaming.value = false;
		}
	}

	return { streaming, error, streamAgent };
}
