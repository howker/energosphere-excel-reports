/*
  Read-only checks for the old test copy of CSD_Astrakhan, SQL Server 2008 R2.
  Collects coefficient function sources, database clocks and nearby tree names.
  No measurement archives are read. INSERT targets are local table variables.
  Save PassportChecksXml as CSD_Astrakhan_passport_checks.xml.
*/
USE [CSD_Astrakhan];
SET NOCOUNT ON;

DECLARE @ConnectionID int;
DECLARE @RuID int;
DECLARE @Packet xml;
SET @ConnectionID = 48803;
SELECT @RuID = ID_Parent FROM dbo.Points
WHERE ID_Point = @ConnectionID AND Point_Type = 10;
IF @RuID IS NULL
BEGIN
    RAISERROR(N'The connection or its parent was not found.', 16, 1);
    RETURN;
END;

DECLARE @Neighbour table (ID_Point int NOT NULL PRIMARY KEY);
BEGIN TRY
    ;WITH Nearby AS
    (
        SELECT ID_Point FROM dbo.Points WHERE ID_Point = @RuID
        UNION ALL
        SELECT p.ID_Point FROM dbo.Points AS p
        INNER JOIN Nearby AS n ON p.ID_Parent = n.ID_Point
    )
    INSERT INTO @Neighbour SELECT DISTINCT ID_Point FROM Nearby
    OPTION (MAXRECURSION 32);
END TRY
BEGIN CATCH
    DECLARE @TreeError nvarchar(2048);
    SET @TreeError = ERROR_MESSAGE();
    RAISERROR(N'Cannot read the bounded neighbouring tree: %s', 16, 1, @TreeError);
    RETURN;
END CATCH;

IF (SELECT COUNT(*) FROM @Neighbour) > 300
BEGIN
    RAISERROR(N'The neighbouring tree exceeds 300 nodes. Export stopped.', 16, 1);
    RETURN;
END;

DECLARE @Modules table (object_id int NOT NULL PRIMARY KEY);
INSERT INTO @Modules
    SELECT o.object_id FROM sys.objects AS o
    WHERE o.schema_id = SCHEMA_ID(N'dbo')
      AND o.type IN ('FN', 'IF', 'TF', 'V')
      AND
      (
          o.name IN (N'MeaCoeff', N'MeaCoeffStr', N'P_IsFeederType', N'GetUTCBias', N'GetCurrentDate')
          OR o.name LIKE N'%UTC%' OR o.name LIKE N'%TimeZone%'
          OR o.name LIKE N'%Bias%' OR o.name LIKE N'%Local%'
      );

IF (SELECT COUNT(*) FROM @Modules) > 100
BEGIN
    RAISERROR(N'The diagnostic module set exceeds 100 objects. Export stopped.', 16, 1);
    RETURN;
END;

SET @Packet =
(
    SELECT
        N'1' AS [@format_version], DB_NAME() AS [@database],
        @ConnectionID AS [@connection_id], @RuID AS [@neighbourhood_root_id],
        (SELECT COUNT(*) FROM @Neighbour) AS [@neighbour_node_count],
        CONVERT(nvarchar(30), GETUTCDATE(), 126) AS [@exported_at_utc],
        (
            SELECT dbo.GetUTCBias() AS UTC_Bias_Minutes,
                CONVERT(nvarchar(30), dbo.GetCurrentDate(), 126) AS DatabaseCurrentDate,
                CONVERT(nvarchar(30), GETUTCDATE(), 126) AS UtcCurrentDate,
                CONVERT(nvarchar(30), GETDATE(), 126) AS ServerCurrentDate
            FOR XML PATH('clocks'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT p.ID_Point, p.PointName, p.ID_Parent, p.Point_Type,
                p.ID_Ref, nt.Name AS NodeTypeName,
                CASE WHEN p.Point_Type = 8 THEN 1 ELSE 0 END AS IsBusNode,
                CASE WHEN p.ID_Point = @ConnectionID THEN 1 ELSE 0 END AS IsTargetConnection
            FROM dbo.Points AS p
            INNER JOIN @Neighbour AS n ON n.ID_Point = p.ID_Point
            LEFT JOIN dbo.PointNodeTypes AS nt ON nt.ID_NodeType = p.Point_Type
            ORDER BY p.ID_Parent, p.ID_Point
            FOR XML PATH('point'), ROOT('neighbour_points'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT pr.ID_Point, pr.ID_Point_Up, pr.Num, pr.LinkType
            FROM dbo.PointRelations AS pr
            WHERE pr.ID_Point IN (48803, 48847, 48815)
            ORDER BY pr.ID_Point, pr.LinkType, pr.Num
            FOR XML PATH('relation'), ROOT('target_relations'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT o.name AS [@name], o.type_desc AS [@type],
                CASE WHEN sm.definition IS NULL THEN 0 ELSE 1 END AS [@definition_available],
                (
                    SELECT sp.parameter_id AS [@id], sp.name AS [@name],
                        TYPE_NAME(sp.user_type_id) AS [@type],
                        sp.max_length AS [@max_length_bytes], sp.is_output AS [@output]
                    FROM sys.parameters AS sp WHERE sp.object_id = o.object_id
                    ORDER BY sp.parameter_id
                    FOR XML PATH('parameter'), ROOT('parameters'), TYPE
                ),
                sm.definition AS [definition]
            FROM sys.objects AS o
            INNER JOIN @Modules AS m ON m.object_id = o.object_id
            LEFT JOIN sys.sql_modules AS sm ON sm.object_id = o.object_id
            ORDER BY o.name
            FOR XML PATH('module'), ROOT('modules'), TYPE
        )
    FOR XML PATH('passport_checks'), TYPE
);

SELECT @Packet AS [PassportChecksXml];
