module.exports = {
  '/remotes/platform': {target:`http://127.0.0.1:${process.env.RUNLOOM_PLATFORM_ADMIN_PORT || 4203}`,pathRewrite:{'^/remotes/platform':''}},
  '/remotes/workflows': {target:`http://127.0.0.1:${process.env.RUNLOOM_WORKFLOWS_PORT || 4201}`,pathRewrite:{'^/remotes/workflows':''}},
  '/remotes/management': {target:`http://127.0.0.1:${process.env.RUNLOOM_MANAGEMENT_PORT || 4202}`,pathRewrite:{'^/remotes/management':''}},
  '/auth': {target:process.env.RUNLOOM_AUTH_PROXY || 'http://127.0.0.1:18081',secure:false,changeOrigin:false},
  '/api': {target:process.env.RUNLOOM_API_PROXY || 'http://127.0.0.1:8080',secure:false,changeOrigin:true},
};
