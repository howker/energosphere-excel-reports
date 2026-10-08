/*
  EnergySphere: passport attribute discovery, SQL Server 2008 R2.
  Reads catalog metadata and five small dictionaries, plus up to 100 candidate
  rows from dbo.Points. Does not execute the exported functions/procedures.
  Does not read dbo.PointMains or other measurement archives.

  SSMS: Ctrl+D; XML data = Unlimited; execute; open PassportDiscoveryXml.
  Save the opened XML as CSD_Astrakhan_passport_discovery.xml.
*/
USE [CSD_Astrakhan];
SET NOCOUNT ON;

IF ISNULL(HAS_PERMS_BY_NAME(DB_NAME(), N'DATABASE', N'VIEW DEFINITION'), 0) <> 1
BEGIN
    RAISERROR(N'VIEW DEFINITION on this database is required to inspect function source.', 16, 1);
    RETURN;
END;

DECLARE @TemplateObjectNumber int;
DECLARE @TemplatePointName nvarchar(300);
DECLARE @Discovery xml;
SET @TemplateObjectNumber = 47462;
SET @TemplatePointName = N'ТСН-2-10 яч.15';

SET @Discovery =
(
    SELECT
        N'1' AS [@format_version],
        DB_NAME() AS [@database],
        CONVERT(nvarchar(30), GETUTCDATE(), 126) AS [@exported_at_utc],
        @TemplateObjectNumber AS [@template_object_number_unverified],
        @TemplatePointName AS [@template_point_name],
        (
            SELECT
                SCHEMA_NAME(o.schema_id) AS [@schema],
                o.name AS [@name],
                o.type_desc AS [@type],
                CASE WHEN sm.definition IS NULL THEN 0 ELSE 1 END AS [@definition_available],
                sm.uses_ansi_nulls AS [@uses_ansi_nulls],
                sm.uses_quoted_identifier AS [@uses_quoted_identifier],
                (
                    SELECT
                        p.parameter_id AS [@id],
                        p.name AS [@name],
                        ty.name AS [@type],
                        SCHEMA_NAME(ty.schema_id) AS [@type_schema],
                        p.max_length AS [@max_length_bytes],
                        p.precision AS [@precision],
                        p.scale AS [@scale],
                        p.is_output AS [@output]
                    FROM sys.parameters AS p
                    INNER JOIN sys.types AS ty ON ty.user_type_id = p.user_type_id
                    WHERE p.object_id = o.object_id
                    ORDER BY p.parameter_id
                    FOR XML PATH('parameter'), ROOT('parameters'), TYPE
                ),
                sm.definition AS [definition]
            FROM sys.objects AS o
            LEFT JOIN sys.sql_modules AS sm ON sm.object_id = o.object_id
            WHERE o.schema_id = SCHEMA_ID(N'dbo')
                AND o.name IN
                (
                    N'GetObjectAttributeDT', N'GetObjectAttribute',
                    N'GetObjectAttributeSimple', N'GetMDObjectAttributeList',
                    N'Meter_Transformators', N'Meter_Transformators_MeterInfo',
                    N'Meter_Transformators_MeaInfo', N'Meter_AttributeFlt',
                    N'Meter_AttributeStr', N'Meter_AttrList',
                    N'P_Parents_List_Base', N'PointPathEx2',
                    N'IntToPhase', N'MeaModuleTypeName', N'GetCurrentDate'
                )
            ORDER BY o.name
            FOR XML PATH('module'), ROOT('modules'), TYPE
        ),
        (
            SELECT
                n.ID_NodeType, n.Name, n.ShortName, n.ConstName,
                n.SphereMask, n.IsVirtual
            FROM dbo.PointNodeTypes AS n
            ORDER BY n.ID_NodeType
            FOR XML PATH('row'), ROOT('point_node_types'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT a.ID_MAT, a.NameLong, a.NameShort
            FROM dbo.M_AttribTypes AS a
            ORDER BY a.ID_MAT
            FOR XML PATH('row'), ROOT('meter_attribute_types'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT e.ID_Type, e.Name, e.Description, e.Inheritance, e.InputType
            FROM dbo.P_Ext_Ids AS e
            ORDER BY e.ID_Type
            FOR XML PATH('row'), ROOT('point_extra_attribute_types'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT ot.ID_MDObjectType, ot.Name
            FROM dbo.MDObjectType AS ot
            ORDER BY ot.ID_MDObjectType
            FOR XML PATH('row'), ROOT('metadata_object_types'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT
                a.ID_MDObjectAttribute, a.ID_MDObjectType, a.Name, a.Description,
                a.IsSystem, a.IsCalculated, a.IsRequired, a.IsReadOnly,
                a.IsAllowedStr, a.IsAllowedNum, a.IsAllowedDate,
                a.IsAllowedFreeValue, a.IsAllowedNewValue, a.DisplayOrder
            FROM dbo.MDObjectAttribute AS a
            ORDER BY a.ID_MDObjectType, a.DisplayOrder, a.ID_MDObjectAttribute
            FOR XML PATH('row'), ROOT('metadata_attributes'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT TOP (100)
                p.ID_Point, p.PointName, p.ID_Parent, p.Point_Type, p.ID_Ref,
                parent.PointName AS DirectParentName,
                CASE WHEN p.ID_Point = @TemplateObjectNumber THEN 1 ELSE 0 END AS MatchesObjectNumber,
                CASE WHEN p.ID_Ref = @TemplateObjectNumber THEN 1 ELSE 0 END AS MatchesReferenceNumber,
                CASE WHEN p.PointName = @TemplatePointName THEN 1 ELSE 0 END AS MatchesExactTemplateName
            FROM dbo.Points AS p
            LEFT JOIN dbo.Points AS parent ON parent.ID_Point = p.ID_Parent
            WHERE p.ID_Point = @TemplateObjectNumber
                OR p.ID_Ref = @TemplateObjectNumber
                OR p.PointName = @TemplatePointName
                OR p.PointName LIKE N'%ТСН-2-10%'
            ORDER BY
                CASE WHEN p.ID_Point = @TemplateObjectNumber THEN 0
                     WHEN p.PointName = @TemplatePointName THEN 1
                     WHEN p.ID_Ref = @TemplateObjectNumber THEN 2 ELSE 3 END,
                p.ID_Point
            FOR XML PATH('point'), ROOT('candidate_points'), ELEMENTS XSINIL, TYPE
        )
    FOR XML PATH('passport_discovery'), TYPE
);

SELECT @Discovery AS [PassportDiscoveryXml];
