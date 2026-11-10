import assert from 'node:assert/strict';
const {chromium}=await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE);
const browser=await chromium.launch({headless:true,...(process.env.RUNLOOM_CHROME?{executablePath:process.env.RUNLOOM_CHROME}:{})});
const page=await browser.newPage({viewport:{width:1440,height:1000}});
page.setDefaultTimeout(10000);
const errors=[], external=[];
page.on('pageerror',error=>errors.push(error.message));
page.on('request',request=>{if(new URL(request.url()).origin!==process.env.RUNLOOM_DOCS_URL)external.push(request.url());});
try {
  for(const [kind,title,path,operation]of [['studio','Runloom Studio API','/api/v1/studio/openapi.json','getProfile'],['consumer','Runloom Consumer Execution API','/api/v1/openapi.json','listWorkflows']]){
    await page.goto(`${process.env.RUNLOOM_DOCS_URL}/api/docs/${kind}/`);
    await page.locator('.swagger-ui .info .title').filter({hasText:title}).waitFor();
    await page.waitForFunction(()=>window.ui?.specSelectors?.loadingStatus()==='success');
    assert.equal(await page.locator('#download-spec').getAttribute('href'),path);
    const config=await page.evaluate(()=>({persist:window.ui.getConfigs().persistAuthorization,validator:window.ui.getConfigs().validatorUrl}));
    assert.deepEqual(config,{persist:false,validator:null});
    assert.equal(await page.locator('.errors-wrapper .errors').count(),0);
    await page.locator(`.opblock-tag[data-tag="${kind==='studio'?'Identity':'Workflows'}"]`).click();
    const block=page.locator(`[id$="-${operation}"]`);
    await block.locator('.opblock-summary').click();
    await block.getByRole('button',{name:'Try it out',exact:true}).click();
    await block.getByRole('button',{name:'Execute',exact:true}).click();
    await block.locator('.live-responses-table .response-col_status').filter({hasText:'401'}).waitFor();
    // Authorize through the actual Swagger dialog using a synthetic token.
    await page.locator('.auth-wrapper .authorize').click();
    const token=kind==='studio'?'admin':'synthetic-documentation-token';
    await page.locator('.dialog-ux input').fill(token);
    await page.locator('.dialog-ux .auth-btn-wrapper .authorize').click();
    await page.locator('.dialog-ux').getByRole('button',{name:'Close',exact:true}).click();
    assert.equal(await page.evaluate(token=>JSON.stringify({...localStorage,...sessionStorage}).includes(token),token),false);
    if(kind==='studio'){
      await block.getByRole('button',{name:'Execute',exact:true}).click();
      await block.locator('.live-responses-table .response-col_status').filter({hasText:'200'}).waitFor();
    }
    await page.reload();
    await page.waitForFunction(()=>window.ui?.specSelectors?.loadingStatus()==='success');
    assert.deepEqual(await page.evaluate(()=>window.ui.authSelectors.authorized().toJS()),{});
    if(kind==='studio'){
      await page.locator('.opblock-tag[data-tag="Events"]').click();
      const resume=page.locator('[id$="-resumeRun"]');
      await resume.locator('.opblock-summary').click();
      await resume.getByRole('button',{name:'Try it out',exact:true}).click();
      assert.equal(await resume.locator('textarea').count(),1);
      const body=JSON.parse(await resume.locator('textarea').inputValue());
      assert.equal(body.step_id,'review-pause');
      assert(!('approved' in body));
    } else assert.equal(await page.locator('.opblock-summary-path').filter({hasText:'/resume'}).count(),0);
  }
  assert.deepEqual(errors,[]);
  assert.deepEqual(external,[],'Swagger must not call CDNs or remote validators');
  if(process.env.RUNLOOM_DOCS_SCREENSHOT)await page.screenshot({path:process.env.RUNLOOM_DOCS_SCREENSHOT,fullPage:true});
  console.log('PASS both Swagger pages, rendered schemas, real handler 401s and authorized 200, request examples, no remote requests and nonpersistent authorization');
} finally {await browser.close();}
