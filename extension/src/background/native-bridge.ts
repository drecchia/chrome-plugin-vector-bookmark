interface NMHandshakeResponse {
	type: 'handshake_ok';
	port: number;
	token: string;
}

export interface DaemonState {
	port: number | null;
	token: string | null;
}

export const daemonState: DaemonState = {
	port: null,
	token: null,
};

export async function connectDaemon(): Promise<void> {
	if (daemonState.port !== null && daemonState.token !== null) return;

	return new Promise((resolve, reject) => {
		chrome.runtime.sendNativeMessage(
			'com.vbm.daemon',
			{ type: 'handshake', extensionId: chrome.runtime.id },
			(response: NMHandshakeResponse | undefined) => {
				if (chrome.runtime.lastError) {
					daemonState.port = null;
					daemonState.token = null;
					reject(
						new Error(
							`Native messaging failed — is the vbm daemon installed? ${chrome.runtime.lastError.message}`,
						),
					);
					return;
				}
				if (!response || response.type !== 'handshake_ok') {
					daemonState.port = null;
					daemonState.token = null;
					reject(
						new Error(
							'Daemon returned unexpected handshake response',
						),
					);
					return;
				}
				daemonState.port = response.port;
				daemonState.token = response.token;
				resolve();
			},
		);
	});
}

export function getDaemonBase(): string {
	return `http://127.0.0.1:${daemonState.port}`;
}

export function getAuthHeader(): string {
	return `Bearer ${daemonState.token}`;
}
