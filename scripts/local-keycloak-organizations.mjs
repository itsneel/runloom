// Bundled local Keycloak only. Add permissions without changing any credential.
export async function ensureLocalOrganizations(baseURL, clientID, fetcher = fetch) {
  const origin = new URL(baseURL);
  if (origin.protocol !== 'http:' || !['localhost', '127.0.0.1', '[::1]'].includes(origin.hostname)) throw new Error('Local organization setup requires loopback Keycloak.');
  const login = await fetcher(baseURL + '/realms/master/protocol/openid-connect/token', {method:'POST',body:new URLSearchParams({grant_type:'password',client_id:'admin-cli',username:'admin',password:'development-only'}),redirect:'error',signal:AbortSignal.timeout(10000)});
  if (!login.ok) throw new Error('Local organization administrator login failed; response omitted.');
  const token = (await login.json()).access_token;
  if (!token) throw new Error('Local organization setup token missing.');
  async function call(method,path,body) {
    const response=await fetcher(baseURL+'/admin/realms/runloom'+path,{method,headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)}),redirect:'error',signal:AbortSignal.timeout(10000)});
    if(!response.ok) throw new Error(`Local organization setup failed (HTTP ${response.status}); provider response omitted.`);
    return response.status===204?undefined:response.json();
  }
  const client=(await call('GET','/clients?clientId='+encodeURIComponent(clientID)))[0];
  if(!client) throw new Error('Local organization provisioner is missing.');
  const serviceUser=await call('GET',`/clients/${client.id}/service-account-user`);
  const management=(await call('GET','/clients?clientId=realm-management'))[0];
  const roles=[];
  for(const role of ['manage-realm','manage-users','view-users','query-users']) roles.push(await call('GET',`/clients/${management.id}/roles/${role}`));
  await call('POST',`/users/${serviceUser.id}/role-mappings/clients/${management.id}`,roles);
  await call('PUT','',{organizationsEnabled:true});
  // Native organizations own membership; the Studio login keeps its existing
  // username/password form and all configured MFA/social-login executions.
  const realm=await call('GET','');
  const flow='/authentication/flows/'+encodeURIComponent(realm.browserFlow)+'/executions';
  const executions=await call('GET',flow);
  for(const execution of executions) {
    if(execution.providerId==='organization' && execution.requirement!=='DISABLED') {
      await call('PUT',flow,{id:execution.id,requirement:'DISABLED'});
    }
  }
}
