/*
  One connection / passport snapshot. SQL Server 2008 R2.
  Source connection verified from the user's discovery XML: dbo.Points 48803.
  All INSERT statements below write only local table variables.
  Reads tree/catalog/equipment tables, not measurement archives.
  SSMS: Ctrl+D; XML data = Unlimited; open OnePassportXml and Save As
  CSD_Astrakhan_one_passport.xml.
*/
USE [CSD_Astrakhan];
SET NOCOUNT ON;

DECLARE @ConnectionID int;
DECLARE @AsOfDB datetime;
DECLARE @MeterIDs varchar(max);
DECLARE @ConnectionPath nvarchar(max);
DECLARE @Packet xml;
SET @ConnectionID = 48803;
SET @AsOfDB = dbo.GetCurrentDate();

IF NOT EXISTS (SELECT 1 FROM dbo.Points WHERE ID_Point = @ConnectionID AND Point_Type = 10)
BEGIN
    RAISERROR(N'The verified connection node was not found. Export stopped.', 16, 1);
    RETURN;
END;

DECLARE @Branch table (ID_Point int NOT NULL PRIMARY KEY);
DECLARE @PathNodes table (ID_Point int NOT NULL PRIMARY KEY, DepthFromConnection int NOT NULL);
DECLARE @Scope table (ID_Point int NOT NULL PRIMARY KEY);
DECLARE @Meters table (ID_Point int NOT NULL PRIMARY KEY);
DECLARE @Transformers table
(
    MeterPointID int NOT NULL,
    TransformerType int NOT NULL,
    TransformerPointID int NOT NULL,
    PRIMARY KEY (MeterPointID, TransformerType, TransformerPointID)
);
DECLARE @Devices table (ID_MeterInfo int NOT NULL PRIMARY KEY);
DECLARE @Types table (ID_Type int NOT NULL PRIMARY KEY);
DECLARE @Calculated table
(
    ID_Object bigint,
    ID_MDObjectAttribute varchar(200),
    ID_MDObjectAttributeValue bigint,
    Value_Str nvarchar(max),
    Value_Date datetime,
    Value_Num decimal(20, 8)
);

BEGIN TRY
    ;WITH Descendants AS
    (
        SELECT p.ID_Point
        FROM dbo.Points AS p WHERE p.ID_Point = @ConnectionID
        UNION ALL
        SELECT p.ID_Point
        FROM dbo.Points AS p
        INNER JOIN Descendants AS d ON p.ID_Parent = d.ID_Point
    )
    INSERT INTO @Branch (ID_Point)
        SELECT DISTINCT ID_Point FROM Descendants
        OPTION (MAXRECURSION 32);

    -- Ordered direct parent chain, including the connection itself.
    ;WITH Ancestors AS
    (
        SELECT p.ID_Point, p.ID_Parent, 0 AS DepthFromConnection
        FROM dbo.Points AS p WHERE p.ID_Point = @ConnectionID
        UNION ALL
        SELECT p.ID_Point, p.ID_Parent, a.DepthFromConnection + 1
        FROM dbo.Points AS p
        INNER JOIN Ancestors AS a ON p.ID_Point = a.ID_Parent
    )
    INSERT INTO @PathNodes (ID_Point, DepthFromConnection)
        SELECT ID_Point, DepthFromConnection FROM Ancestors
        OPTION (MAXRECURSION 32);
END TRY
BEGIN CATCH
    DECLARE @TreeError nvarchar(2048);
    SET @TreeError = ERROR_MESSAGE();
    RAISERROR(N'Cannot read the bounded connection tree: %s', 16, 1, @TreeError);
    RETURN;
END CATCH;

IF (SELECT COUNT(*) FROM @Branch) > 300
BEGIN
    RAISERROR(N'The connection branch exceeds the 300-node diagnostic limit. Export stopped.', 16, 1);
    RETURN;
END;

INSERT INTO @Scope SELECT ID_Point FROM @Branch;

INSERT INTO @Scope
    SELECT pn.ID_Point FROM @PathNodes AS pn
    WHERE NOT EXISTS (SELECT 1 FROM @Scope AS s WHERE s.ID_Point = pn.ID_Point);

SET @ConnectionPath = STUFF
(
    (SELECT N' ' + LTRIM(RTRIM(CONVERT(nvarchar(300), p.PointName)))
     FROM @PathNodes AS pn
     INNER JOIN dbo.Points AS p ON p.ID_Point = pn.ID_Point
     WHERE LEN(LTRIM(RTRIM(p.PointName))) > 0
     ORDER BY pn.DepthFromConnection DESC
     FOR XML PATH(''), TYPE).value('.', 'nvarchar(max)'),
    1, 1, N''
);

-- Ancestors and alternate links registered by the existing application.
INSERT INTO @Scope
    SELECT DISTINCT pr.ID_Point_Up
    FROM dbo.PointRelations AS pr
    INNER JOIN @Branch AS b ON b.ID_Point = pr.ID_Point
    WHERE pr.LinkType IN (1, 2, 3)
        AND NOT EXISTS (SELECT 1 FROM @Scope AS s WHERE s.ID_Point = pr.ID_Point_Up);

-- Resolve one hop of explicit references, matching Meter_Transformators logic.
INSERT INTO @Scope
    SELECT DISTINCT p.ID_Ref
    FROM dbo.Points AS p
    INNER JOIN @Scope AS s ON s.ID_Point = p.ID_Point
    WHERE p.Point_Type = 255 AND p.ID_Ref IS NOT NULL
        AND NOT EXISTS (SELECT 1 FROM @Scope AS already WHERE already.ID_Point = p.ID_Ref);

-- Meter/supply nodes in this branch, or the connection itself if it has mount history.
INSERT INTO @Meters
    SELECT p.ID_Point
    FROM dbo.Points AS p
    WHERE
        (
            p.Point_Type IN (21, 149)
            AND
            (
                EXISTS (SELECT 1 FROM @Branch AS b WHERE b.ID_Point = p.ID_Point)
                OR EXISTS
                (
                    SELECT 1 FROM dbo.Points AS link
                    INNER JOIN @Branch AS b ON b.ID_Point = link.ID_Point
                    WHERE link.Point_Type = 255 AND link.ID_Ref = p.ID_Point
                )
            )
        )
        OR
        (
            p.ID_Point = @ConnectionID
            AND EXISTS (SELECT 1 FROM dbo.MeterMountHist AS h WHERE h.ID_Point = p.ID_Point)
        );

-- The inspected function resolves sibling TT/TN nodes and references to them.
INSERT INTO @Transformers
    SELECT m.ID_Point, 1, t.ID_Point
    FROM @Meters AS m
    CROSS APPLY dbo.Meter_Transformators(m.ID_Point, 1) AS t
    UNION
    SELECT m.ID_Point, 2, t.ID_Point
    FROM @Meters AS m
    CROSS APPLY dbo.Meter_Transformators(m.ID_Point, 2) AS t;

INSERT INTO @Scope
    SELECT DISTINCT t.TransformerPointID
    FROM @Transformers AS t
    WHERE NOT EXISTS (SELECT 1 FROM @Scope AS s WHERE s.ID_Point = t.TransformerPointID);

IF (SELECT COUNT(*) FROM @Scope) > 600
BEGIN
    RAISERROR(N'The linked object scope exceeds the 600-node diagnostic limit. Export stopped.', 16, 1);
    RETURN;
END;

-- Preserve past and current mount records to verify the installation chronology.
IF
(
    SELECT COUNT(*) FROM dbo.MeterMountHist AS h
    INNER JOIN @Scope AS s ON s.ID_Point = h.ID_Point
) > 2000
BEGIN
    RAISERROR(N'Mount history exceeds the 2000-row diagnostic limit. Export stopped.', 16, 1);
    RETURN;
END;

INSERT INTO @Devices
    SELECT DISTINCT h.ID_MeterInfo
    FROM dbo.MeterMountHist AS h
    INNER JOIN @Scope AS s ON s.ID_Point = h.ID_Point
    WHERE h.ID_MeterInfo IS NOT NULL;

INSERT INTO @Types
    SELECT DISTINCT mi.ID_ModuleType
    FROM dbo.MeterInfo AS mi
    INNER JOIN @Devices AS d ON d.ID_MeterInfo = mi.ID_MeterInfo
    WHERE mi.ID_ModuleType IS NOT NULL;

SET @MeterIDs = STUFF
(
    (SELECT ',' + CONVERT(varchar(20), m.ID_Point)
     FROM @Meters AS m ORDER BY m.ID_Point
     FOR XML PATH(''), TYPE).value('.', 'varchar(max)'),
    1, 1, ''
);

IF ISNULL(@MeterIDs, '') <> ''
    INSERT INTO @Calculated
        SELECT a.ID_Object, a.ID_MDObjectAttribute, a.ID_MDObjectAttributeValue,
            a.Value_Str, a.Value_Date, a.Value_Num
        FROM dbo.GetObjectAttributeDT
        (
            1, @MeterIDs,
            '0,3,4,100,101,102,103,108,120,127,128,132,133,134,135,136,137,179,181,182',
            @AsOfDB
        ) AS a;

SET @Packet =
(
    SELECT
        N'1' AS [@format_version],
        DB_NAME() AS [@database],
        @ConnectionID AS [@connection_id],
        @ConnectionPath AS [@full_connection_path],
        CONVERT(nvarchar(30), @AsOfDB, 126) AS [@as_of_database_time],
        CONVERT(nvarchar(30), GETUTCDATE(), 126) AS [@exported_at_utc],
        (SELECT COUNT(*) FROM @Branch) AS [@branch_node_count],
        (SELECT COUNT(*) FROM @Scope) AS [@scope_node_count],
        (SELECT COUNT(*) FROM @Meters) AS [@meter_node_count],
        (
            SELECT pn.DepthFromConnection, p.ID_Point, p.PointName,
                p.ID_Parent, p.Point_Type
            FROM @PathNodes AS pn
            INNER JOIN dbo.Points AS p ON p.ID_Point = pn.ID_Point
            ORDER BY pn.DepthFromConnection DESC
            FOR XML PATH('node'), ROOT('connection_path'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT p.ID_Point, p.PointName, p.ID_Parent, p.Point_Type, nt.Name AS NodeTypeName,
                p.ID_Ref,
                CASE WHEN b.ID_Point IS NULL THEN 0 ELSE 1 END AS InConnectionBranch,
                CASE WHEN m.ID_Point IS NULL THEN 0 ELSE 1 END AS SelectedMeterNode
            FROM dbo.Points AS p
            INNER JOIN @Scope AS s ON s.ID_Point = p.ID_Point
            LEFT JOIN @Branch AS b ON b.ID_Point = p.ID_Point
            LEFT JOIN @Meters AS m ON m.ID_Point = p.ID_Point
            LEFT JOIN dbo.PointNodeTypes AS nt ON nt.ID_NodeType = p.Point_Type
            ORDER BY p.ID_Point
            FOR XML PATH('point'), ROOT('points'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT pr.ID_Point, pr.ID_Point_Up, pr.Num, pr.LinkType
            FROM dbo.PointRelations AS pr
            INNER JOIN @Branch AS b ON b.ID_Point = pr.ID_Point
            ORDER BY pr.ID_Point, pr.LinkType, pr.Num, pr.ID_Point_Up
            FOR XML PATH('relation'), ROOT('point_relations'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT t.MeterPointID, t.TransformerType, t.TransformerPointID
            FROM @Transformers AS t
            ORDER BY t.MeterPointID, t.TransformerType, t.TransformerPointID
            FOR XML PATH('link'), ROOT('transformer_links'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT h.ID_MMH, h.ID_Point, h.ID_MeterInfo, h.ID_Module,
                h.DT_Mount, h.DT_Dismount, h.Reason, h.Phase,
                CASE WHEN h.DT_Mount <= @AsOfDB AND @AsOfDB < h.DT_Dismount THEN 1 ELSE 0 END AS ActiveAtAsOf
            FROM dbo.MeterMountHist AS h
            INNER JOIN @Scope AS s ON s.ID_Point = h.ID_Point
            ORDER BY h.ID_Point, h.DT_Mount, h.Phase, h.ID_MMH
            FOR XML PATH('mount'), ROOT('mount_history'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT mi.ID_MeterInfo, mi.ID_ModuleType, mi.SN, mi.SN_Display,
                mi.DT_QC_Prev, mi.DT_QC_Next, mi.DT_Verify
            FROM dbo.MeterInfo AS mi
            INNER JOIN @Devices AS d ON d.ID_MeterInfo = mi.ID_MeterInfo
            ORDER BY mi.ID_MeterInfo
            FOR XML PATH('device'), ROOT('devices'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT mt.ID_Type, mt.NameType, mt.DisplayName, mt.ID_BaseType, mt.Point_Type
            FROM dbo.ModuleTypes AS mt
            INNER JOIN @Types AS t ON t.ID_Type = mt.ID_Type
            ORDER BY mt.ID_Type
            FOR XML PATH('type'), ROOT('device_types'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT ma.ID_MA, ma.ID_MT, ma.ID_MeterInfo, ma.ID_MAT,
                mat.NameLong, mat.NameShort, ma.Val_Str, ma.Val_Flt
            FROM dbo.MeterAttributes AS ma
            LEFT JOIN dbo.M_AttribTypes AS mat ON mat.ID_MAT = ma.ID_MAT
            WHERE
                (
                    EXISTS (SELECT 1 FROM @Devices AS d WHERE d.ID_MeterInfo = ma.ID_MeterInfo)
                    OR
                    (
                        ma.ID_MeterInfo IS NULL
                        AND EXISTS (SELECT 1 FROM @Types AS t WHERE t.ID_Type = ma.ID_MT)
                    )
                )
                AND ma.ID_MAT IN (6,7,8,9,10,11,62,66,67,70,73,99,100,101,102,103,104,105,106,107,108,109)
            ORDER BY ma.ID_MeterInfo, ma.ID_MT, ma.ID_MAT, ma.ID_MA
            FOR XML PATH('attribute'), ROOT('device_attributes'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT ei.ID_EI, ei.ID_Point, ei.ID_Type, et.Name AS AttributeName,
                ei.Data_Str, ei.Data_Int,
                es.ID_ES AS DictionaryValueID, es.Data_Str AS DictionaryValueText
            FROM dbo.P_Ext_Inf AS ei
            INNER JOIN @Scope AS s ON s.ID_Point = ei.ID_Point
            LEFT JOIN dbo.P_Ext_Ids AS et ON et.ID_Type = ei.ID_Type
            LEFT JOIN dbo.P_Ext_Src AS es ON es.ID_Type = ei.ID_Type AND es.Data_Int = ei.Data_Int
            ORDER BY ei.ID_Point, ei.ID_Type, ei.ID_EI, es.ID_ES
            FOR XML PATH('attribute'), ROOT('point_extra_attributes'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT a.ID_Object, a.ID_MDObjectAttribute, md.Name AS AttributeName,
                a.ID_MDObjectAttributeValue, a.Value_Str, a.Value_Date, a.Value_Num
            FROM @Calculated AS a
            LEFT JOIN dbo.MDObjectAttribute AS md
                ON CONVERT(varchar(200), md.ID_MDObjectAttribute) = a.ID_MDObjectAttribute
                AND md.ID_MDObjectType = 1
            ORDER BY a.ID_Object, a.ID_MDObjectAttribute
            FOR XML PATH('attribute'), ROOT('calculated_attributes'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT o.name AS [@name], o.type_desc AS [@type],
                CASE WHEN sm.definition IS NULL THEN 0 ELSE 1 END AS [@definition_available],
                sm.definition AS [definition]
            FROM sys.objects AS o
            LEFT JOIN sys.sql_modules AS sm ON sm.object_id = o.object_id
            WHERE o.schema_id = SCHEMA_ID(N'dbo') AND o.name IN
                (N'Get_MeaCoeff_Periods', N'GetUTCBias', N'vwCURRENT_TIMESTAMP',
                 N'P_Parents_List', N'GetMeterVoltageClass')
            ORDER BY o.name
            FOR XML PATH('module'), ROOT('support_modules'), TYPE
        )
    FOR XML PATH('one_passport'), TYPE
);

SELECT @Packet AS [OnePassportXml];
