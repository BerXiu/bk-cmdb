<template>
    <div class="template-layout">
        <div class="options clearfix">
            <div class="fl">
                <span class="fl" v-cursor="{
                    active: !$isAuthorized($OPERATION.C_SET_TEMPLATE),
                    auth: [$OPERATION.C_SET_TEMPLATE]
                }">
                    <bk-button
                        theme="primary"
                        :disabled="!$isAuthorized($OPERATION.C_SET_TEMPLATE)"
                        @click="handleCreate"
                    >
                        {{$t('新建')}}
                    </bk-button>
                </span>
                <span class="fl" v-cursor="{
                    active: !$isAuthorized($OPERATION.D_SET_TEMPLATE),
                    auth: [$OPERATION.D_SET_TEMPLATE]
                }">
                    <bk-button
                        theme="default"
                        class="ml10"
                        :disabled="!checkedIds.length || !$isAuthorized($OPERATION.D_SET_TEMPLATE)"
                        @click="handleBatchDelete"
                    >
                        {{$t('批量删除')}}
                    </bk-button>
                </span>
            </div>
            <div class="fr">
                <bk-input :placeholder="$t('模板名称')"
                    right-icon="icon-search"
                    v-model="searchName"
                    @enter="handleFilterTemplate"></bk-input>
            </div>
        </div>
        <bk-table class="template-table"
            :data="list"
            :row-style="{ cursor: 'pointer' }"
            @selection-change="handleSelectionChange"
            @row-click="handleRowClick">
            <bk-table-column type="selection" width="50" :selectable="handleSelectable"></bk-table-column>
            <bk-table-column :label="$t('模板名称')" prop="name" class-name="is-highlight"></bk-table-column>
            <bk-table-column :label="$t('应用数量')" prop="set_instance_count"></bk-table-column>
            <bk-table-column :label="$t('修改人')" prop="modifier"></bk-table-column>
            <bk-table-column :label="$t('修改时间')" prop="last_time">
                <template slot-scope="{ row }">
                    <span>{{$tools.formatTime(row.last_time, 'YYYY-MM-DD HH:mm')}}</span>
                </template>
            </bk-table-column>
            <bk-table-column :label="$t('操作')" width="180">
                <template slot-scope="{ row }">
                    <span
                        v-cursor="{
                            active: !$isAuthorized($OPERATION.U_SET_TEMPLATE),
                            auth: [$OPERATION.U_SET_TEMPLATE]
                        }">
                        <bk-button
                            text
                            :disabled="!$isAuthorized($OPERATION.U_SET_TEMPLATE)"
                            @click="handleEdit(row)"
                        >
                            {{$t('编辑')}}
                        </bk-button>
                    </span>
                    <bk-button text class="ml15" @click="handlePreview(row)">{{$t('预览')}}</bk-button>
                    <span class="text-primary ml15"
                        style="color: #dcdee5 !important; cursor: not-allowed;"
                        v-if="row.set_instance_count && $isAuthorized($OPERATION.D_SET_TEMPLATE)"
                        v-bk-tooltips.top="$t('不可删除')">
                        {{$t('删除')}}
                    </span>
                    <span v-else
                        v-cursor="{
                            active: !$isAuthorized($OPERATION.D_SET_TEMPLATE),
                            auth: [$OPERATION.D_SET_TEMPLATE]
                        }">
                        <bk-button text class="ml15"
                            :disabled="!$isAuthorized($OPERATION.D_SET_TEMPLATE)"
                            @click="handleDelete(row)"
                        >
                            {{$t('删除')}}
                        </bk-button>
                    </span>
                </template>
            </bk-table-column>
            <template slot="empty">
                <i class="bk-table-empty-icon bk-icon icon-empty"></i>
                <i18n path="空集群模板提示" tag="div">
                    <span
                        place="link"
                        v-cursor="{
                            active: !$isAuthorized($OPERATION.C_SET_TEMPLATE),
                            auth: [$OPERATION.C_SET_TEMPLATE]
                        }">
                        <bk-button
                            text
                            :disabled="!$isAuthorized($OPERATION.C_SET_TEMPLATE)"
                            @click="handleCreate"
                        >
                            {{$t('立即创建')}}
                        </bk-button>
                    </span>
                </i18n>
            </template>
        </bk-table>
        <bk-dialog
            header-position="left"
            :draggable="false"
            :width="759"
            :title="dialog.title"
            v-model="dialog.visible"
            @after-leave="handleDialogClose">
            <component
                :is="dialog.component"
                v-bind="dialog.props">
            </component>
            <template slot="footer">
                <bk-button @click="dialog.visible = false">{{$t('关闭')}}</bk-button>
            </template>
        </bk-dialog>
    </div>
</template>

<script>
    import cmdbSetTemplateTree from './children/template-tree.vue'
    export default {
        components: {
            cmdbSetTemplateTree
        },
        data () {
            return {
                list: [],
                originList: [],
                searchName: '',
                checkedIds: [],
                dialog: {
                    visible: false,
                    title: '',
                    component: null,
                    props: {}
                }
            }
        },
        computed: {
            business () {
                return this.$store.state.objectBiz.bizId
            }
        },
        async created () {
            await this.getSetTemplates()
        },
        methods: {
            async getSetTemplates () {
                const data = await this.$store.dispatch('setTemplate/getSetTemplates', {
                    bizId: this.business,
                    params: {},
                    config: {
                        requestId: 'getSetTemplates'
                    }
                })
                const list = (data.info || []).map(item => ({
                    set_instance_count: item.set_instance_count,
                    ...item.set_template
                }))
                this.list = list
                this.originList = list
            },
            handleCreate () {
                this.$router.push({
                    name: 'setTemplateConfig',
                    params: {
                        mode: 'create'
                    }
                })
            },
            handleEdit (row) {
                this.$router.push({
                    name: 'setTemplateConfig',
                    params: {
                        mode: 'edit',
                        templateId: row.id
                    }
                })
            },
            handlePreview (row) {
                this.dialog.props = {
                    mode: 'view',
                    templateId: row.id
                }
                this.dialog.title = row.name
                this.dialog.component = 'cmdb-set-template-tree'
                this.dialog.visible = true
            },
            handleDialogClose () {
                this.dialog.props = {}
                this.dialog.title = ''
                this.dialog.component = null
            },
            async handleDelete (row) {
                this.$bkInfo({
                    title: this.$t('确认删除xx集群模板', { name: row.name }),
                    confirmFn: async () => {
                        try {
                            await this.$store.dispatch('setTemplate/deleteSetTemplate', {
                                bizId: this.$store.getters['objectBiz/bizId'],
                                config: {
                                    data: {
                                        set_template_ids: [row.id]
                                    }
                                }
                            })
                            this.getSetTemplates()
                        } catch (e) {
                            console.error(e)
                        }
                    }
                })
            },
            async handleBatchDelete () {
                this.$bkInfo({
                    title: this.$t('确认删除选中的集群模板'),
                    confirmFn: async () => {
                        try {
                            await this.$store.dispatch('setTemplate/deleteSetTemplate', {
                                bizId: this.$store.getters['objectBiz/bizId'],
                                config: {
                                    data: {
                                        set_template_ids: this.checkedIds
                                    }
                                }
                            })
                            this.getSetTemplates()
                        } catch (e) {
                            console.error(e)
                        }
                    }
                })
            },
            handleFilterTemplate () {
                const originList = this.$tools.clone(this.originList)
                this.list = this.searchName
                    ? originList.filter(template => template.name.indexOf(this.searchName) !== -1)
                    : originList
            },
            handleSelectable (row) {
                return !row.set_instance_count
            },
            handleSelectionChange (selection) {
                this.checkedIds = selection.map(item => item.id)
            },
            handleRowClick (row, event, column) {
                if (!column.property) {
                    return false
                }
                this.$router.push({
                    name: 'setTemplateConfig',
                    params: {
                        mode: 'view',
                        templateId: row.id
                    }
                })
            }
        }
    }
</script>

<style lang="scss" scoped>
    .template-layout {
        padding: 0 20px;
    }
    .options {
        font-size: 0;
    }
    .template-table {
        margin-top: 16px;
    }
</style>