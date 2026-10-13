// The caller waits for this before starting Go; HTTP failures never become an
// empty catalogue. Go validates the format and the build's expected digest.
export async function fetchActionCatalogue(url: string, fetcher: typeof fetch = fetch): Promise<string> {
	const response = await fetcher(url);
	if (!response.ok) throw new Error(`Could not load action catalogue: HTTP ${response.status}`);
	return response.text();
}
