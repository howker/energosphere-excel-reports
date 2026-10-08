/*
  Validate legacy date display and native coefficients for one test passport.
  SQL Server 2008 R2. Uses existing scalar/table functions; no persistent writes.
  Save PassportValidationXml as CSD_Astrakhan_passport_validation.xml.
*/
USE [CSD_Astrakhan];
SET NOCOUNT ON;

DECLARE @AsOf datetime;
DECLARE @Packet xml;
SET @AsOf = dbo.GetCurrentDate();

IF NOT EXISTS (SELECT 1 FROM dbo.Points WHERE ID_Point = 48847 AND Point_Type = 21)
BEGIN
    RAISERROR(N'The verified test meter node was not found.', 16, 1);
    RETURN;
END;

DECLARE @Modules table (object_id int NOT NULL PRIMARY KEY);
INSERT INTO @Modules
    SELECT o.object_id FROM sys.objects AS o
    WHERE o.schema_id = SCHEMA_ID(N'dbo')
      AND o.name IN (N'IsDayLight', N'GPSToLocalTime', N'LocalTimeToGPS',
                     N'MAT_PT_KTR', N'P_Type', N'DT_Intersect');

-- Include two levels of SQL function/view dependencies for these helpers.
DECLARE @Step int;
SET @Step = 0;
WHILE @Step < 2
BEGIN
    INSERT INTO @Modules
        SELECT DISTINCT o.object_id
        FROM sys.sql_expression_dependencies AS d
        INNER JOIN @Modules AS m ON m.object_id = d.referencing_id
        INNER JOIN sys.objects AS o ON o.object_id = d.referenced_id
        WHERE o.type IN ('FN', 'IF', 'TF', 'V')
          AND NOT EXISTS (SELECT 1 FROM @Modules AS already WHERE already.object_id = o.object_id);
    SET @Step = @Step + 1;
END;
IF (SELECT COUNT(*) FROM @Modules) > 100
BEGIN
    RAISERROR(N'The helper module set exceeds 100 objects. Export stopped.', 16, 1);
    RETURN;
END;

SET @Packet =
(
    SELECT N'1' AS [@format_version], DB_NAME() AS [@database],
        48803 AS [@connection_id],
        CONVERT(nvarchar(30), @AsOf, 126) AS [@as_of_database_time],
        CONVERT(nvarchar(30), GETUTCDATE(), 126) AS [@exported_at_utc],
        (
            SELECT h.ID_MMH, h.ID_Point, h.ID_MeterInfo, h.Phase,
                mi.SN_Display, mi.DT_QC_Prev AS RawVerificationDate,
                dbo.GPSToLocalTime(mi.DT_QC_Prev) AS SeasonalVerificationDate,
                CONVERT(nvarchar(10), dbo.GPSToLocalTime(mi.DT_QC_Prev), 104) AS SeasonalVerificationDateText,
                mi.DT_QC_Next AS RawNextVerificationDate,
                dbo.GPSToLocalTime(mi.DT_QC_Next) AS SeasonalNextVerificationDate,
                CONVERT(nvarchar(10), dbo.GPSToLocalTime(mi.DT_QC_Next), 104) AS SeasonalNextVerificationDateText
            FROM dbo.MeterMountHist AS h
            INNER JOIN dbo.MeterInfo AS mi ON mi.ID_MeterInfo = h.ID_MeterInfo
            WHERE h.ID_Point IN (48847, 48846, 48815)
              AND h.DT_Mount <= @AsOf AND @AsOf < h.DT_Dismount
            ORDER BY h.ID_Point, h.Phase, h.ID_MMH
            FOR XML PATH('device'), ROOT('date_checks'), ELEMENTS XSINIL, TYPE
        ),
        (
            -- Synthetic control timestamps, not passport values.
            SELECT v.ProbeName, v.RawDate,
                dbo.GPSToLocalTime(v.RawDate) AS SeasonalDate
            FROM
            (
                SELECT N'winter_2026_test_only' AS ProbeName,
                    CONVERT(datetime, '20260216', 112) AS RawDate
                UNION ALL
                SELECT N'summer_2026_test_only', CONVERT(datetime, '20260714', 112)
                UNION ALL
                SELECT N'future_winter_2033_test_only', CONVERT(datetime, '20330216', 112)
                UNION ALL
                SELECT N'future_summer_2033_test_only', CONVERT(datetime, '20330714', 112)
            ) AS v
            ORDER BY v.RawDate
            FOR XML PATH('probe'), ROOT('synthetic_conversion_probes'), TYPE
        ),
        (
            SELECT c.DT_From, c.DT_To, c.Coeff, c.Coeff_I, c.Coeff_U,
                c.Coeff_I_Str, c.Coeff_U_Str
            FROM dbo.Get_MeaCoeff_Periods(48847, @AsOf, @AsOf) AS c
            FOR XML PATH('interval'), ROOT('native_coefficients'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT h.ID_Point, h.ID_MeterInfo, h.Phase, mi.SN_Display,
                dbo.MeaCoeffStr(NULL, h.ID_MeterInfo, NULL, @AsOf) AS NativeInstanceRatio
            FROM dbo.MeterMountHist AS h
            INNER JOIN dbo.MeterInfo AS mi ON mi.ID_MeterInfo = h.ID_MeterInfo
            WHERE h.ID_Point IN (48846, 48815)
              AND h.DT_Mount <= @AsOf AND @AsOf < h.DT_Dismount
            ORDER BY h.ID_Point, h.Phase, h.ID_MMH
            FOR XML PATH('device'), ROOT('native_instance_ratios'), ELEMENTS XSINIL, TYPE
        ),
        (
            SELECT o.name AS [@name], o.type_desc AS [@type],
                CASE WHEN sm.definition IS NULL THEN 0 ELSE 1 END AS [@definition_available],
                sm.definition AS [definition]
            FROM sys.objects AS o
            INNER JOIN @Modules AS m ON m.object_id = o.object_id
            LEFT JOIN sys.sql_modules AS sm ON sm.object_id = o.object_id
            ORDER BY o.name
            FOR XML PATH('module'), ROOT('modules'), TYPE
        ),
        (
            SELECT OBJECT_NAME(d.referencing_id) AS ReferencingModule,
                d.referenced_database_name, d.referenced_schema_name,
                d.referenced_entity_name, d.referenced_id
            FROM sys.sql_expression_dependencies AS d
            INNER JOIN @Modules AS m ON m.object_id = d.referencing_id
            ORDER BY ReferencingModule, d.referenced_entity_name
            FOR XML PATH('dependency'), ROOT('helper_dependencies'), ELEMENTS XSINIL, TYPE
        )
    FOR XML PATH('passport_validation'), TYPE
);

SELECT @Packet AS [PassportValidationXml];
