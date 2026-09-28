import type { RouteRecordRaw } from 'vue-router'
export const knowledgeOpsRoute: RouteRecordRaw = {
  path: 'knowledge-ops',
  name: 'knowledgeOps',
  component: () => import('@/views/knowledge-ops/Workbench.vue'),
  meta: { requiresInit: true, requiresAuth: true, requiresTenantAdmin: true },
}
export const knowledgeOpsNavigation = {
  title: '', titleKey: 'menu.knowledgeOps', icon: 'knowledge-ops', path: 'knowledge-ops',
}
