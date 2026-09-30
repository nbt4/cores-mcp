package mcpserver

const warehouseDeviceFixtureSQL = `CREATE TABLE warehouse_schema_migrations(version TEXT PRIMARY KEY);
CREATE TABLE products(productid INT PRIMARY KEY,name TEXT,lifecycle_status TEXT,tracking_mode TEXT);
INSERT INTO products VALUES(1,'Device Product','active','individual'),(2,'Other Product','active','individual'),(3,'Bulk','active','bulk'),(4,'Archived','archived','individual');
CREATE SEQUENCE device_master_code_seq;
CREATE TABLE devices(deviceid VARCHAR(50) PRIMARY KEY,productid INT REFERENCES products(productid),serialnumber VARCHAR(255),barcode VARCHAR(255),qr_code VARCHAR(255),condition_rating NUMERIC(3,1) DEFAULT 5,usage_hours NUMERIC(10,2) DEFAULT 0,purchasedate DATE,lastmaintenance DATE,nextmaintenance DATE,notes TEXT,status TEXT,condition_status TEXT,current_location TEXT,zone_id INT,current_case_id INT,lifecycle_status VARCHAR(20) NOT NULL DEFAULT 'active',archived_at TIMESTAMP,archived_by_product BOOLEAN DEFAULT false,updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);
CREATE FUNCTION generate_device_id() RETURNS TRIGGER AS $$ BEGIN IF NEW.deviceid IS NULL OR TRIM(NEW.deviceid)='' THEN NEW.deviceid := 'DEV-' || LPAD(nextval('device_master_code_seq')::text,8,'0'); END IF; IF NEW.barcode IS NULL OR TRIM(NEW.barcode)='' THEN NEW.barcode := NEW.deviceid; END IF; IF NEW.qr_code IS NULL OR TRIM(NEW.qr_code)='' THEN NEW.qr_code := 'WH:' || NEW.deviceid; END IF; RETURN NEW; END $$ LANGUAGE plpgsql;
CREATE TRIGGER devices_generate_id BEFORE INSERT ON devices FOR EACH ROW EXECUTE FUNCTION generate_device_id();
CREATE TABLE inventory_identifiers(entity_type TEXT,entity_key TEXT,identifier_kind TEXT,code TEXT,active BOOLEAN,UNIQUE(entity_type,entity_key,identifier_kind));
CREATE UNIQUE INDEX uq_active_code ON inventory_identifiers(lower(trim(code))) WHERE active;
CREATE TABLE storage_zones(zone_id INT PRIMARY KEY,parent_zone_id INT,code TEXT,name TEXT,is_active BOOLEAN,is_storable BOOLEAN,operational_status TEXT,capacity NUMERIC);
INSERT INTO storage_zones VALUES(1,NULL,'STORE','Store',true,true,'available',1),(2,NULL,'BLOCK','Blocked',true,true,'blocked',NULL),(3,NULL,'AREA','Area',true,false,'available',NULL);
CREATE TABLE cases(caseid INT,zone_id INT);
CREATE TABLE product_locations(zone_id INT,quantity NUMERIC);
CREATE TABLE status(statusid INT PRIMARY KEY,status TEXT);
INSERT INTO status VALUES(1,'open'),(2,'closed');
CREATE FUNCTION warehouse_job_status_is_closed(TEXT) RETURNS BOOLEAN LANGUAGE SQL IMMUTABLE AS $$ SELECT $1='closed' $$;
CREATE TABLE jobs(jobid INT,statusid INT,deleted_at TIMESTAMP);
INSERT INTO jobs VALUES(1,1,NULL),(2,2,NULL);
CREATE TABLE job_devices(deviceid TEXT,jobid INT,pack_status TEXT);
CREATE TABLE job_positions(position_id INT,job_id INT);
INSERT INTO job_positions VALUES(1,1);
CREATE TABLE job_position_devices(device_id TEXT,position_id INT);
CREATE TABLE job_package_reservations(device_id TEXT,reservation_status TEXT);
CREATE TABLE devicescases(deviceid TEXT);
CREATE TABLE device_components(device_id TEXT,component_device_id TEXT);
CREATE TABLE warehouse_tasks(device_id TEXT,status TEXT);
CREATE TABLE maintenance_orders(device_id TEXT,status TEXT);
CREATE TABLE maintenance_plans(device_id TEXT,is_active BOOLEAN);
CREATE TABLE defect_reports(device_id TEXT,status TEXT);
CREATE TABLE device_movements(device_id TEXT);
CREATE TABLE audit_log(id BIGSERIAL PRIMARY KEY,user_id BIGINT,action TEXT,entity_type TEXT,entity_id TEXT,old_values JSONB,new_values JSONB,user_agent TEXT,timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE warehouse_product_mutation_receipts(id BIGSERIAL PRIMARY KEY,user_id BIGINT NOT NULL,operation VARCHAR(80) NOT NULL,key_hash CHAR(64) NOT NULL,request_hash CHAR(64) NOT NULL,response JSONB NOT NULL DEFAULT '{}'::jsonb,status_code INTEGER NOT NULL DEFAULT 200,created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,UNIQUE(user_id,operation,key_hash));
`
