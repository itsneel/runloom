// Generate a fresh one-time request without saving files or calling the API.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

export function oneTimeGreeting(delayMinutes = 5, now = new Date()) {
  if (!Number.isInteger(delayMinutes) || delayMinutes < 1 || delayMinutes > 1440) {
    throw new Error('Delay must be a whole number of minutes from 1 to 1440.');
  }
  if (!(now instanceof Date) || !Number.isFinite(now.getTime())) throw new Error('Invalid current time.');
  const sample = JSON.parse(readFileSync(new URL('./hourly-greeting.json', import.meta.url), 'utf8'));
  delete sample.interval_seconds;
  return {
    ...sample,
    name: 'Sample one-time greeting',
    at: new Date(now.getTime() + delayMinutes * 60000).toISOString(),
  };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    if (process.argv.length > 3) throw new Error('Usage: node one-time-greeting.mjs [delay-minutes]');
    const delay = process.argv[2] === undefined ? 5 : Number(process.argv[2]);
    process.stdout.write(`${JSON.stringify(oneTimeGreeting(delay), null, 2)}\n`);
  } catch (error) {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  }
}
