// Single-node development MongoDB only. Never reconfigure an existing replica set.
export const mongoDNSGate = `
resolved=0
for attempt in {1..60}; do
  if getent ahostsv4 "$RUNLOOM_MONGO_HOST" | awk '{print $1}' | grep -Fxq "$RUNLOOM_POD_IP"; then
    resolved=1
    break
  fi
  sleep 2
done
if [ "$resolved" != 1 ]; then
  echo 'MongoDB startup waiting for its headless Service DNS to resolve to this pod' >&2
  exit 1
fi
`;

export function mongoProbe(host, initialize = false) {
  return `
const check = () => {
try {
  const authenticated = db.getSiblingDB('admin').auth('runloom_root', require('fs').readFileSync('/run/secrets/database-root-password', 'utf8').trim());
  if (!authenticated || authenticated.ok === 0) return 1;
  const hello = db.hello();
  if (hello.isWritablePrimary && hello.setName === 'runloom') return 0;
  ${initialize ? `try {
    rs.status();
  } catch (error) {
    if (error.code === 94) {
      // Only a genuinely uninitialized data directory may be initialized.
      const result = rs.initiate({_id: 'runloom', members: [{_id: 0, host: ${JSON.stringify(host)}}]});
      if (result.ok !== 1) return 1;
    }
  }` : ''}
} catch (error) {
  // Never emit connection details or credentials from probes.
}
return 1;
};
quit(check());
`;
}
