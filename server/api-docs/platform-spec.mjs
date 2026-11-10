export function platformSpec() {
  const string = {type:'string'};
  const object = properties => ({type:'object',properties,additionalProperties:false});
  const role = {type:'string',enum:['','platform_admin','platform_staff']};
  const page = items => object({items:{type:'array',items},total:{type:'integer'},offset:{type:'integer'},limit:{type:'integer'}});
  const childPolicy = object({max_children:{type:'integer',minimum:0,maximum:10},default_max_children:{type:'integer',minimum:0,maximum:10},override:{type:['integer','null'],minimum:0,maximum:10},maximum:{type:'integer',const:10}});
  const metadata = object({id:string,name:string,team_count:{type:'integer'},member_count:{type:'integer'},parent_id:string,child_count:{type:'integer'},child_limit:childPolicy});
  const member = object({id:string,name:string,email:{type:'string',format:'email'},role});
  const event = object(Object.fromEntries(['actor','target','action','previous_role','role','reason','created_at'].map(k=>[k,string])));
  const get = (operationId,summary,permission,schema,paginated=false) => ({operationId,summary,description:'Required permission: '+permission+'. Platform roles do not grant tenant-content access.',security:[{platformBearer:[]}],
    ...(paginated?{parameters:[{name:'offset',in:'query',schema:{type:'integer',minimum:0,default:0}},{name:'limit',in:'query',schema:{type:'integer',minimum:1,maximum:100,default:50}}]}:{}),
    responses:{200:{description:'Success',content:{'application/json':{schema}}},400:{description:'Invalid request or last administrator protection'},401:{description:'Verified human Studio token required; machine tokens rejected'},403:{description:'Insufficient platform permission or recent MFA required'},503:{description:'Identity database unavailable'}}});
  const assign = {...get('setPlatformRole','Assign or remove platform access','platform.staff.manage',object({saved:{type:'boolean'}})),requestBody:{required:true,content:{'application/json':{schema:{...object({email:{type:'string',format:'email'},role,reason:{type:'string',minLength:3,maxLength:300}}),required:['email','role','reason']}}}}};
  const childOperation = (id, summary, write) => ({...get(id,summary,write?'platform.organizations.configure':'platform.organizations.read',childPolicy),parameters:[{name:'org',in:'path',required:true,schema:string}],description:'Root organizations only. Limits count children excluding the parent. Overrides replace the platform default, allow 0–10, and preserve existing children. Changes are audited. Keycloak administrators can override Runloom creation rules.'});
  return {openapi:'3.1.0',info:{title:'Runloom Platform API',version:'1.0.0',description:'Separate platform administration. No team header is required. Existing users only; role changes are audited atomically and checked on every request. Hosted access requires operator-configured MFA. Password-only access is local opt-in.'},servers:[{url:'/api/v1/platform'}],components:{securitySchemes:{platformBearer:{type:'http',scheme:'bearer',bearerFormat:'JWT'}}},paths:{
    '/me':{get:get('getPlatformIdentity','Read own role and permissions','any verified human; no MFA step-up for discovery',object({role,permissions:{type:'array',items:string}}))},
    '/organizations':{get:get('listPlatformOrganizations','List organization metadata','platform.organizations.read',page(metadata),true)},
    '/organization-settings':{get:get('getOrganizationSettings','Read child organization creation policy','platform.organizations.configure',object({max_children:{type:'integer',minimum:0,maximum:10},maximum:{type:'integer'}})),put:{...get('setOrganizationSettings','Set child organization limit; preserve existing organizations','platform.organizations.configure',object({max_children:{type:'integer'},maximum:{type:'integer'}})),requestBody:{required:true,content:{'application/json':{schema:{...object({max_children:{type:'integer',minimum:0,maximum:10}}),required:['max_children']}}}}}},
    '/organizations/{org}/child-limit':{
      get:childOperation('getOrganizationChildLimit','Read effective organization child limit',false),
      put:{...childOperation('setOrganizationChildLimit','Override child limit for one organization',true),requestBody:{required:true,content:{'application/json':{schema:{...object({max_children:{type:'integer',minimum:0,maximum:10}}),required:['max_children']}}}}},
      delete:childOperation('resetOrganizationChildLimit','Restore platform default for one organization',true),
    },
    '/staff':{get:get('listPlatformStaff','List platform assignments','platform.staff.manage',page(member),true),put:assign},
    '/audit':{get:get('listPlatformAudit','List newest platform access changes first','platform.audit.read',page(event),true)},
    '/health':{get:get('getPlatformHealth','Check configured service readiness','platform.health.read',object({checked_at:{type:'string',format:'date-time'},items:{type:'array',items:object({service:string,status:{type:'string',enum:['healthy','degraded','unavailable','unknown']},detail:string,latency_ms:{type:'integer'}})}}))},
  }};
}
