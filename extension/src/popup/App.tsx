import React, { useEffect, useState } from 'react';
import type { StatusResponse, ForgetRequest } from '../../../proto/types';
import {
	DEFAULT_HOST,
	DEFAULT_PORT,
	saveDaemonConfig,
} from '../background/native-bridge';

export default function App() {
	const [status, setStatus] = useState<StatusResponse | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [loading, setLoading] = useState(true);
	const [forgetValue, setForgetValue] = useState('');
	const [forgetType, setForgetType] = useState<'url' | 'domain'>('url');
	const [forgetMsg, setForgetMsg] = useState<string | null>(null);
	const [host, setHost] = useState(DEFAULT_HOST);
	const [port, setPort] = useState(String(DEFAULT_PORT));
	const [configSaved, setConfigSaved] = useState(false);

	useEffect(() => {
		chrome.storage.local.get(['vbmHost', 'vbmPort'], (result) => {
			if (result.vbmHost) setHost(result.vbmHost as string);
			if (result.vbmPort) setPort(String(result.vbmPort));
		});
	}, []);

	useEffect(() => {
		chrome.runtime.sendMessage({ type: 'popup_status' }, (res) => {
			setLoading(false);
			if (chrome.runtime.lastError) {
				setError(chrome.runtime.lastError.message ?? 'Unknown error');
				return;
			}
			if (res && res.error) {
				setError(res.error);
			} else {
				setStatus(res as StatusResponse);
			}
		});
	}, []);

	// P1-01: send pause/resume to service worker.
	function handleToggleCapture() {
		const nextEnabled = !(status?.captureEnabled ?? true);
		chrome.runtime.sendMessage(
			{ type: 'popup_set_capture', enabled: nextEnabled },
			(res) => {
				if (res?.ok) {
					setStatus((s) =>
						s ? { ...s, captureEnabled: res.captureEnabled } : s,
					);
				}
			},
		);
	}

	function handleSaveConfig() {
		const p = parseInt(port, 10);
		if (!host.trim() || isNaN(p) || p < 1 || p > 65535) return;
		saveDaemonConfig({ host: host.trim(), port: p }).then(() => {
			setConfigSaved(true);
			setTimeout(() => setConfigSaved(false), 2000);
		});
	}

	function handleForget() {
		if (!forgetValue.trim()) return;
		const req: ForgetRequest = {
			type: forgetType,
			value: forgetValue.trim(),
		};
		chrome.runtime.sendMessage({ type: 'popup_forget', req }, (res) => {
			if (res?.ok) {
				setForgetMsg('Forgotten.');
				setForgetValue('');
			} else {
				setForgetMsg(`Error: ${res?.error ?? 'unknown'}`);
			}
			setTimeout(() => setForgetMsg(null), 3000);
		});
	}

	const paused = !(status?.captureEnabled ?? true);

	const container: React.CSSProperties = {
		padding: '12px 16px',
		display: 'flex',
		flexDirection: 'column',
		gap: '10px',
		boxSizing: 'border-box',
		overflow: 'hidden',
	};

	const header: React.CSSProperties = {
		display: 'flex',
		justifyContent: 'space-between',
		alignItems: 'center',
	};

	const title: React.CSSProperties = {
		fontWeight: 700,
		fontSize: '15px',
		margin: 0,
	};

	const pauseBtn: React.CSSProperties = {
		fontSize: '11px',
		padding: '3px 8px',
		border: '1px solid #ccc',
		borderRadius: '4px',
		cursor: 'pointer',
		background: paused ? '#fee2e2' : '#f0fdf4',
		color: paused ? '#dc2626' : '#16a34a',
	};

	const statusRow: React.CSSProperties = {
		fontSize: '13px',
		color: '#374151',
		display: 'flex',
		gap: '12px',
	};

	const gray: React.CSSProperties = { color: '#9ca3af' };
	const errorBox: React.CSSProperties = {
		fontSize: '12px',
		color: '#991b1b',
		background: '#fef2f2',
		border: '1px solid #fecaca',
		borderRadius: '4px',
		padding: '6px 8px',
		lineHeight: '1.4',
	};

	const sectionLabel: React.CSSProperties = {
		fontSize: '11px',
		fontWeight: 600,
		color: '#6b7280',
		letterSpacing: '0.02em',
	};

	const row: React.CSSProperties = { display: 'flex', gap: '6px' };

	const input: React.CSSProperties = {
		flex: 1,
		minWidth: 0,
		fontSize: '12px',
		padding: '5px 8px',
		border: '1px solid #d1d5db',
		borderRadius: '4px',
		outline: 'none',
		boxSizing: 'border-box',
	};

	const select: React.CSSProperties = {
		flexShrink: 0,
		fontSize: '12px',
		padding: '5px 6px',
		border: '1px solid #d1d5db',
		borderRadius: '4px',
		background: '#fff',
	};

	const btn: React.CSSProperties = {
		flexShrink: 0,
		fontSize: '12px',
		padding: '5px 10px',
		border: 'none',
		borderRadius: '4px',
		background: '#ef4444',
		color: '#fff',
		cursor: 'pointer',
	};

	const linkBtn: React.CSSProperties = {
		fontSize: '12px',
		color: '#2563eb',
		background: 'none',
		border: 'none',
		cursor: 'pointer',
		padding: 0,
		textDecoration: 'underline',
		textAlign: 'left',
	};

	const footer: React.CSSProperties = {
		fontSize: '11px',
		color: '#9ca3af',
		borderTop: '1px solid #f3f4f6',
		paddingTop: '8px',
		marginTop: '2px',
	};

	return (
		<div style={container}>
			<div style={header}>
				<p style={title}>🔖 Vector Bookmark</p>
				<button style={pauseBtn} onClick={handleToggleCapture}>
					{paused ? 'Resume' : 'Pause'}
				</button>
			</div>

			{loading && (
				<div style={{ fontSize: '12px', color: '#9ca3af' }}>
					Loading...
				</div>
			)}

			{error && <div style={errorBox}>{error}</div>}

			{status && (
				<div style={statusRow}>
					<span>
						<strong>{status.indexed}</strong> pages indexed
					</span>
					<span style={status.pending === 0 ? gray : undefined}>
						<strong>{status.pending}</strong> pending
					</span>
				</div>
			)}

			<button
				style={linkBtn}
				onClick={() => {
					chrome.tabs.create({
						url: `http://${host}:${port}/ui`,
					});
				}}
			>
				Open full UI
			</button>

			<div>
				<div style={sectionLabel}>Daemon</div>
				<div style={{ ...row, marginTop: '6px' }}>
					<input
						style={{ ...input, flex: '1 1 120px' }}
						type="text"
						placeholder="host"
						value={host}
						onChange={(e) => setHost(e.target.value)}
						onKeyDown={(e) =>
							e.key === 'Enter' && handleSaveConfig()
						}
					/>
					<input
						style={{ ...input, flex: '0 0 60px' }}
						type="number"
						placeholder="port"
						value={port}
						onChange={(e) => setPort(e.target.value)}
						onKeyDown={(e) =>
							e.key === 'Enter' && handleSaveConfig()
						}
					/>
					<button
						style={{ ...btn, background: '#6b7280' }}
						onClick={handleSaveConfig}
					>
						Save
					</button>
				</div>
				{configSaved && (
					<div
						style={{
							fontSize: '11px',
							color: '#6b7280',
							marginTop: '4px',
						}}
					>
						Saved. Reload extension to reconnect.
					</div>
				)}
			</div>

			<div>
				<div style={sectionLabel}>Forget</div>
				<div style={{ ...row, marginTop: '6px' }}>
					<input
						style={input}
						type="text"
						placeholder="URL or domain..."
						value={forgetValue}
						onChange={(e) => setForgetValue(e.target.value)}
						onKeyDown={(e) => e.key === 'Enter' && handleForget()}
					/>
					<select
						style={select}
						value={forgetType}
						onChange={(e) =>
							setForgetType(e.target.value as 'url' | 'domain')
						}
					>
						<option value="url">URL</option>
						<option value="domain">Domain</option>
					</select>
					<button style={btn} onClick={handleForget}>
						Forget
					</button>
				</div>
				{forgetMsg && (
					<div
						style={{
							fontSize: '11px',
							color: '#6b7280',
							marginTop: '4px',
						}}
					>
						{forgetMsg}
					</div>
				)}
			</div>

			<div style={footer}>Type @recall in address bar to search</div>
		</div>
	);
}
