/*
  EnergySphere / CSD_Astrakhan: analytical schema inventory.
  Target syntax: SQL Server 2008 R2.
  Reads system catalog views only. No application table rows are exported.
  This XML is an inventory for analysis, not a restorable database backup.

  Run in SSMS with Results to Grid (Ctrl+D).
  Set Query Options > Results > Grid > XML data = Unlimited.
  Open the XML result link, then Save As: CSD_Astrakhan_schema.xml.
*/
USE [CSD_Astrakhan];
SET NOCOUNT ON;

IF ISNULL(HAS_PERMS_BY_NAME(DB_NAME(), N'DATABASE', N'VIEW DEFINITION'), 0) <> 1
BEGIN
    RAISERROR(N'Full schema visibility requires VIEW DEFINITION on this database. Export stopped to avoid an incomplete inventory.', 16, 1);
    RETURN;
END;

DECLARE @SchemaInventory xml;

SET @SchemaInventory =
(
    SELECT
        N'1' AS [@format_version],
        DB_NAME() AS [@database],
        CONVERT(nvarchar(128), SERVERPROPERTY(N'ProductVersion')) AS [@server_version],
        CONVERT(nvarchar(128), SERVERPROPERTY(N'Edition')) AS [@server_edition],
        CONVERT(nvarchar(30), GETUTCDATE(), 126) AS [@exported_at_utc],
        N'catalog_only' AS [@content],
        (SELECT d.compatibility_level FROM sys.databases AS d
         WHERE d.database_id = DB_ID()) AS [@compatibility_level],
        (SELECT COUNT(*) FROM sys.tables WHERE is_ms_shipped = 0) AS [@table_count],
        (SELECT COUNT(*) FROM sys.foreign_keys WHERE is_ms_shipped = 0) AS [@foreign_key_count],
        (
            SELECT
                ts.name AS [@schema],
                t.name AS [@name],
                t.object_id AS [@object_id],
                (SELECT COALESCE(SUM(p.rows), 0) FROM sys.partitions AS p
                 WHERE p.object_id = t.object_id AND p.index_id IN (0, 1)) AS [@approx_rows],
                (
                    SELECT
                        c.column_id AS [@id],
                        c.name AS [@name],
                        tys.name AS [@type_schema],
                        ty.name AS [@type],
                        bt.name AS [@base_type],
                        c.max_length AS [@max_length_bytes],
                        c.precision AS [@precision],
                        c.scale AS [@scale],
                        c.is_nullable AS [@nullable],
                        c.is_identity AS [@identity],
                        c.is_computed AS [@computed],
                        c.is_rowguidcol AS [@rowguid],
                        c.collation_name AS [@collation],
                        CONVERT(nvarchar(100), ic.seed_value) AS [@identity_seed],
                        CONVERT(nvarchar(100), ic.increment_value) AS [@identity_increment],
                        cc.is_persisted AS [@computed_persisted],
                        dc.name AS [@default_constraint],
                        dc.definition AS [default_expression],
                        cc.definition AS [computed_expression],
                        CONVERT(nvarchar(max), ep.value) AS [description]
                    FROM sys.columns AS c
                    INNER JOIN sys.types AS ty ON ty.user_type_id = c.user_type_id
                    INNER JOIN sys.schemas AS tys ON tys.schema_id = ty.schema_id
                    LEFT JOIN sys.types AS bt
                        ON bt.user_type_id = c.system_type_id
                        AND bt.user_type_id = bt.system_type_id
                    LEFT JOIN sys.identity_columns AS ic
                        ON ic.object_id = c.object_id AND ic.column_id = c.column_id
                    LEFT JOIN sys.computed_columns AS cc
                        ON cc.object_id = c.object_id AND cc.column_id = c.column_id
                    LEFT JOIN sys.default_constraints AS dc ON dc.object_id = c.default_object_id
                    LEFT JOIN sys.extended_properties AS ep
                        ON ep.class = 1 AND ep.major_id = c.object_id
                        AND ep.minor_id = c.column_id AND ep.name = N'MS_Description'
                    WHERE c.object_id = t.object_id
                    ORDER BY c.column_id
                    FOR XML PATH('column'), ROOT('columns'), TYPE
                ),
                (
                    SELECT
                        kc.name AS [@name],
                        kc.type_desc AS [@type],
                        kc.unique_index_id AS [@index_id]
                    FROM sys.key_constraints AS kc
                    WHERE kc.parent_object_id = t.object_id
                    ORDER BY kc.name
                    FOR XML PATH('key'), ROOT('keys'), TYPE
                ),
                (
                    SELECT
                        i.index_id AS [@id],
                        i.name AS [@name],
                        i.type_desc AS [@type],
                        i.is_unique AS [@unique],
                        i.is_primary_key AS [@primary_key],
                        i.is_unique_constraint AS [@unique_constraint],
                        i.is_disabled AS [@disabled],
                        i.has_filter AS [@filtered],
                        i.filter_definition AS [filter_expression],
                        (
                            SELECT
                                ix.index_column_id AS [@position],
                                col.name AS [@name],
                                ix.key_ordinal AS [@key_ordinal],
                                ix.is_descending_key AS [@descending],
                                ix.is_included_column AS [@included],
                                ix.partition_ordinal AS [@partition_ordinal]
                            FROM sys.index_columns AS ix
                            LEFT JOIN sys.columns AS col
                                ON col.object_id = ix.object_id AND col.column_id = ix.column_id
                            WHERE ix.object_id = i.object_id AND ix.index_id = i.index_id
                            ORDER BY ix.index_column_id
                            FOR XML PATH('column'), ROOT('columns'), TYPE
                        )
                    FROM sys.indexes AS i
                    WHERE i.object_id = t.object_id AND i.index_id > 0 AND i.is_hypothetical = 0
                    ORDER BY i.index_id
                    FOR XML PATH('index'), ROOT('indexes'), TYPE
                ),
                (
                    SELECT
                        ck.name AS [@name],
                        ck.is_disabled AS [@disabled],
                        ck.is_not_trusted AS [@not_trusted],
                        ck.definition AS [expression]
                    FROM sys.check_constraints AS ck
                    WHERE ck.parent_object_id = t.object_id
                    ORDER BY ck.name
                    FOR XML PATH('check'), ROOT('checks'), TYPE
                ),
                (
                    SELECT CONVERT(nvarchar(max), tp.value) AS [text()]
                    FROM sys.extended_properties AS tp
                    WHERE tp.class = 1 AND tp.major_id = t.object_id
                        AND tp.minor_id = 0 AND tp.name = N'MS_Description'
                    FOR XML PATH('description'), TYPE
                )
            FROM sys.tables AS t
            INNER JOIN sys.schemas AS ts ON ts.schema_id = t.schema_id
            WHERE t.is_ms_shipped = 0
            ORDER BY ts.name, t.name
            FOR XML PATH('table'), ROOT('tables'), TYPE
        ),
        (
            SELECT
                fk.name AS [@name],
                SCHEMA_NAME(child.schema_id) AS [@child_schema],
                child.name AS [@child_table],
                SCHEMA_NAME(parent.schema_id) AS [@parent_schema],
                parent.name AS [@parent_table],
                fk.is_disabled AS [@disabled],
                fk.is_not_trusted AS [@not_trusted],
                fk.delete_referential_action_desc AS [@on_delete],
                fk.update_referential_action_desc AS [@on_update],
                (
                    SELECT
                        fc.constraint_column_id AS [@position],
                        child_col.name AS [@child_column],
                        parent_col.name AS [@parent_column]
                    FROM sys.foreign_key_columns AS fc
                    INNER JOIN sys.columns AS child_col
                        ON child_col.object_id = fc.parent_object_id
                        AND child_col.column_id = fc.parent_column_id
                    INNER JOIN sys.columns AS parent_col
                        ON parent_col.object_id = fc.referenced_object_id
                        AND parent_col.column_id = fc.referenced_column_id
                    WHERE fc.constraint_object_id = fk.object_id
                    ORDER BY fc.constraint_column_id
                    FOR XML PATH('column_pair'), ROOT('columns'), TYPE
                )
            FROM sys.foreign_keys AS fk
            INNER JOIN sys.tables AS child ON child.object_id = fk.parent_object_id
            INNER JOIN sys.tables AS parent ON parent.object_id = fk.referenced_object_id
            WHERE fk.is_ms_shipped = 0
            ORDER BY SCHEMA_NAME(child.schema_id), child.name, fk.name
            FOR XML PATH('foreign_key'), ROOT('foreign_keys'), TYPE
        ),
        (
            SELECT
                os.name AS [@schema],
                o.name AS [@name],
                o.type_desc AS [@type],
                o.object_id AS [@object_id],
                o.parent_object_id AS [@parent_object_id]
            FROM sys.objects AS o
            INNER JOIN sys.schemas AS os ON os.schema_id = o.schema_id
            WHERE o.is_ms_shipped = 0
                AND o.type IN ('V', 'P', 'PC', 'FN', 'IF', 'TF', 'FS', 'FT', 'TR', 'TA', 'SN')
            ORDER BY os.name, o.type, o.name
            FOR XML PATH('object'), ROOT('other_objects'), TYPE
        ),
        (
            SELECT
                SCHEMA_NAME(o.schema_id) AS [@referencing_schema],
                o.name AS [@referencing_object],
                o.type_desc AS [@referencing_type],
                d.referencing_minor_id AS [@referencing_column_id],
                d.referenced_class_desc AS [@referenced_class],
                d.referenced_server_name AS [@referenced_server],
                d.referenced_database_name AS [@referenced_database],
                COALESCE(d.referenced_schema_name, SCHEMA_NAME(ro.schema_id)) AS [@referenced_schema],
                d.referenced_entity_name AS [@referenced_name],
                d.referenced_minor_id AS [@referenced_column_id],
                d.is_schema_bound_reference AS [@schema_bound],
                d.is_caller_dependent AS [@caller_dependent],
                d.is_ambiguous AS [@ambiguous]
            FROM sys.sql_expression_dependencies AS d
            INNER JOIN sys.objects AS o ON o.object_id = d.referencing_id
            LEFT JOIN sys.objects AS ro ON ro.object_id = d.referenced_id
            WHERE o.is_ms_shipped = 0
            ORDER BY SCHEMA_NAME(o.schema_id), o.name, d.referenced_entity_name,
                d.referencing_minor_id, d.referenced_minor_id
            FOR XML PATH('dependency'), ROOT('expression_dependencies'), TYPE
        )
    FOR XML PATH('database_schema'), TYPE
);

SELECT @SchemaInventory AS [SchemaXml];
