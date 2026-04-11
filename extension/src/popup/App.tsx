import React, { useEffect, useState } from 'react';

interface StatusResponse {
	indexed: number;
	pending: number;
	version: string;
}

interface ForgetRequest {
	type: 'url' | 'domain' | 'timerange';
	value: string;
}

export default function App() {
	const [status, setStatus] = useState<StatusResponse | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [paused, setPaused] = useState(false);
	const [loading, setLoading] = useState(true);
	const [forgetValue, setForgetValue] = useState('');
	const [forgetType, setForgetType] = useState<'url' | 'domain'>('url');
	const [forgetMsg, setForgetMsg] = useState<string | null>(null);

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

	const container: React.CSSProperties = {
		padding: '12px 16px',
		display: 'flex',
		flexDirection: 'column',
		gap: '10px',
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
	const red: React.CSSProperties = { color: '#dc2626', fontSize: '12px' };

	const sectionLabel: React.CSSProperties = {
		fontSize: '11px',
		fontWeight: 600,
		textTransform: 'uppercase',
		color: '#6b7280',
		letterSpacing: '0.05em',
	};

	const row: React.CSSProperties = { display: 'flex', gap: '6px' };

	const input: React.CSSProperties = {
		flex: 1,
		fontSize: '12px',
		padding: '4px 8px',
		border: '1px solid #d1d5db',
		borderRadius: '4px',
		outline: 'none',
	};

	const select: React.CSSProperties = {
		fontSize: '12px',
		padding: '4px 6px',
		border: '1px solid #d1d5db',
		borderRadius: '4px',
		background: '#fff',
	};

	const btn: React.CSSProperties = {
		fontSize: '12px',
		padding: '4px 10px',
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
	};

	return (
		<div style={container}>
			<div style={header}>
				<p style={title}>🔖 Vector Bookmark</p>
				<button style={pauseBtn} onClick={() => setPaused((p) => !p)}>
					{paused ? 'Resume' : 'Pause'}
				</button>
			</div>

			{loading && (
				<div style={{ fontSize: '12px', color: '#9ca3af' }}>
					Loading...
				</div>
			)}

			{error && <div style={red}>Error: {error}</div>}

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
					const port = 7700; // fallback; daemon provides real port
					chrome.tabs.create({ url: `http://127.0.0.1:${port}/ui` });
				}}
			>
				Open full UI
			</button>

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
