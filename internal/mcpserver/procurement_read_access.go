package mcpserver

// procurementRequisitionReadAccess mirrors ProcurementCore's requester/admin
// rule. Every query aliases the parent requisition as r. The identity is bound
// by Store.Query with transaction-local set_config, never interpolated in SQL.
// Current database rights, rather than cached token claims, govern each read.
// Missing, inactive and machine identities cannot read requisitions.
const procurementRequisitionReadAccess = `EXISTS (
 SELECT 1 FROM users access_user
 WHERE access_user.userid::text=current_setting('cores.user_id', true)
 AND access_user.is_active
 AND (access_user.is_admin OR r.requester_id=access_user.userid))`
