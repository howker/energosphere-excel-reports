"""Build an offline passport data sample from the two verified SQL XML exports."""
import argparse
from datetime import datetime
from decimal import Decimal
import hashlib
import json
from pathlib import Path
import re
import xml.etree.ElementTree as ET


def records(root, section):
    node = root.find(section)
    if node is None:
        raise ValueError(f'Missing XML section: {section}')
    return [{c.tag: c.text for c in row} for row in node]


def decimal_text(value):
    return format(Decimal(value).normalize(), 'f')


def load_snapshot(path):
    if path.suffix.lower() != '.json':
        return ET.parse(path).getroot(), None
    # Accept the saved structured extract when the original upload is unavailable.
    evidence = json.loads(path.read_text(encoding='utf-8'))
    root = ET.Element('one_passport', evidence['export'])
    sections = {name: ET.SubElement(root, name) for name in
                ('connection_path', 'points', 'devices', 'device_types',
                 'device_attributes', 'mount_history', 'calculated_attributes')}
    def add(section, values):
        row = ET.SubElement(sections[section], 'record')
        for key, value in values.items():
            ET.SubElement(row, key).text = value
    for p in evidence['connection_path']:
        add('connection_path', p)
    seen_devices, seen_types, seen_attributes = set(), set(), set()
    for e in evidence['equipment']:
        mount, device = e['mount'], e['device']
        add('mount_history', mount)
        add('points', {'ID_Point': mount['ID_Point'], 'Point_Type': e['point_type']})
        if device['ID_MeterInfo'] not in seen_devices:
            add('devices', device)
            seen_devices.add(device['ID_MeterInfo'])
        if device['ID_ModuleType'] not in seen_types:
            add('device_types', {'ID_Type': device['ID_ModuleType'], 'NameType': e['type_name']})
            seen_types.add(device['ID_ModuleType'])
        for attribute_id, a in e['attributes'].items():
            if a['ID_MA'] is None or a['ID_MA'] in seen_attributes:
                continue
            seen_attributes.add(a['ID_MA'])
            add('device_attributes', {'ID_MA': a['ID_MA'], 'ID_MAT': attribute_id,
                'ID_MeterInfo': device['ID_MeterInfo'] if a['source'] == 'instance' else None,
                'ID_MT': device['ID_ModuleType'] if a['source'] == 'type' else None,
                'Val_Flt': a['value'], 'Val_Str': None})
    for a in evidence['calculated_attributes']:
        add('calculated_attributes', a)
    return root, evidence['source_sha256']


def build(snapshot_path, validation_path):
    snapshot, original_snapshot_hash = load_snapshot(snapshot_path)
    validation = ET.parse(validation_path).getroot()
    if snapshot.tag != 'one_passport' or validation.tag != 'passport_validation':
        raise ValueError('Unexpected XML document types')
    if (snapshot.get('database'), snapshot.get('connection_id')) != (
            validation.get('database'), validation.get('connection_id')):
        raise ValueError('The exports refer to different databases or connections')

    path = records(snapshot, 'connection_path')
    if not path or path[-1]['ID_Point'] != snapshot.get('connection_id'):
        raise ValueError('The connection path is empty or has the wrong endpoint')
    if path[0]['ID_Parent'] is not None or any(
            child['ID_Parent'] != parent['ID_Point'] for parent, child in zip(path, path[1:])):
        raise ValueError('The connection path is incomplete or out of order')
    full_path = ' '.join(p['PointName'].strip() for p in path if p['PointName'].strip())
    if full_path != snapshot.get('full_connection_path'):
        raise ValueError('The exported path text disagrees with the path nodes')
    filename_stem = re.sub(r'\s+', ' ', re.sub(r'[<>:"/\\|?*\x00-\x1f]', ' ', full_path)).strip().rstrip('. ')
    if not filename_stem:
        raise ValueError('The filename stem is empty')

    devices = {d['ID_MeterInfo']: d for d in records(snapshot, 'devices')}
    types = {t['ID_Type']: t for t in records(snapshot, 'device_types')}
    points = {p['ID_Point']: p for p in records(snapshot, 'points')}
    attributes = records(snapshot, 'device_attributes')
    checks = {r['ID_MMH']: r for r in records(validation, 'date_checks')}
    ratios = {(r['ID_Point'], r['ID_MeterInfo'], r['Phase']): r['NativeInstanceRatio']
              for r in records(validation, 'native_instance_ratios')}

    def effective(device, attribute_id, numeric=True):
        field = 'Val_Flt' if numeric else 'Val_Str'
        for instance in (True, False):
            matches = [a for a in attributes if a['ID_MAT'] == str(attribute_id)
                       and (a['ID_MeterInfo'] == device['ID_MeterInfo'] if instance else
                            a['ID_MeterInfo'] is None and a['ID_MT'] == device['ID_ModuleType'])
                       and a[field] is not None]
            if len(matches) > 1:
                raise ValueError(f'Ambiguous attribute {device["ID_MeterInfo"]}/{attribute_id}')
            if matches:
                a = matches[0]
                return {'value': decimal_text(a[field]) if numeric else a[field],
                        'attribute_row_id': a['ID_MA'], 'source': 'instance' if instance else 'type'}
        # Native Meter_AttributeStr has a special fallback for attribute 6.
        if not numeric and attribute_id == 6:
            return {'value': types[device['ID_ModuleType']]['NameType'],
                    'attribute_row_id': None, 'source': 'type_name'}
        return {'value': None, 'attribute_row_id': None, 'source': None}

    equipment = []
    for mount in records(snapshot, 'mount_history'):
        if mount['ActiveAtAsOf'] != '1' or mount['ID_MeterInfo'] is None:
            continue
        d = devices[mount['ID_MeterInfo']]
        check = checks.get(mount['ID_MMH'])
        if check is None or check['ID_MeterInfo'] != d['ID_MeterInfo'] or check['SN_Display'] != d['SN_Display']:
            raise ValueError('Installation identity changed between exports')
        if check['RawVerificationDate'] != d['DT_QC_Prev'] or check['RawNextVerificationDate'] != d['DT_QC_Next']:
            raise ValueError('Verification dates changed between exports')
        kind = {'21': 'meter', '81': 'current_transformer', '85': 'voltage_transformer'}.get(
            points[mount['ID_Point']]['Point_Type'], 'other')
        e = {'kind': kind, 'point_id': mount['ID_Point'], 'device_id': d['ID_MeterInfo'],
             'mount_id': mount['ID_MMH'], 'phase_code': mount['Phase'],
             'phase': {'1': 'A', '2': 'B', '3': 'C'}.get(mount['Phase']),
             'type': effective(d, 6, False), 'serial_number': d['SN_Display'],
             'verification_interval_years': effective(d, 99),
             'verification_date_raw': d['DT_QC_Prev'],
             'verification_date_database_function': check['SeasonalVerificationDate'],
             'verification_date_database_function_text': check['SeasonalVerificationDateText'],
             'next_verification_date_raw': d['DT_QC_Next'],
             'next_verification_date_database_function': check['SeasonalNextVerificationDate'],
             'report_verification_date': datetime.fromisoformat(d['DT_QC_Prev']).strftime('%d.%m.%Y') if d['DT_QC_Prev'] else None,
             'accuracy_active': effective(d, 7) if kind == 'meter' else None,
             'accuracy_reactive': effective(d, 8) if kind == 'meter' else None,
             'accuracy_transformer': effective(d, 105) if kind.endswith('_transformer') else None}
        if kind.endswith('_transformer'):
            ids = (103, 104) if kind == 'current_transformer' else (101, 102)
            e['primary_nominal'], e['secondary_nominal'] = [effective(d, i) for i in ids]
            primary, secondary = e['primary_nominal']['value'], e['secondary_nominal']['value']
            native = ratios.get((mount['ID_Point'], mount['ID_MeterInfo'], mount['Phase']))
            e['native_ratio'] = native
            if primary is not None and secondary is not None and Decimal(secondary) > 0:
                e['coefficient_from_attributes'] = decimal_text(Decimal(primary) / Decimal(secondary))
                if native is None or [Decimal(v.strip()) for v in native.split('/')] != [Decimal(primary), Decimal(secondary)]:
                    raise ValueError('Native ratio disagrees with effective instance attributes')
        equipment.append(e)

    calculated = records(snapshot, 'calculated_attributes')
    bus_values = {a['Value_Str'] for a in calculated if a['ID_MDObjectAttribute'] == '103' and a['Value_Str']}
    account_values = {a['Value_Str'] for a in calculated if a['ID_MDObjectAttribute'] == '4' and a['Value_Str']}
    coefficients = records(validation, 'native_coefficients')
    for c in coefficients:
        if Decimal(c['Coeff']) != Decimal(c['Coeff_I']) * Decimal(c['Coeff_U']):
            raise ValueError('Native total coefficient disagrees with its components')
    return {'format_version': 1, 'data_authority': 'EnergySphere',
            'source_kind': 'old_test_database_copy', 'connection_id': snapshot.get('connection_id'),
            'database': snapshot.get('database'), 'snapshot_time': snapshot.get('as_of_database_time'),
            'validation_time': validation.get('as_of_database_time'),
            'date_rule': 'format raw database date without timezone or seasonal shifts',
            'original_snapshot_sha256_from_evidence': original_snapshot_hash,
            'source_files': [{'name': p.name, 'sha256': hashlib.sha256(p.read_bytes()).hexdigest()}
                             for p in (snapshot_path, validation_path)],
            'connection_path': path, 'full_connection_path': full_path, 'filename_stem': filename_stem,
            'bus_name': next(iter(bus_values)) if len(bus_values) == 1 else None,
            'accounting_type': next(iter(account_values)) if len(account_values) == 1 else None,
            'equipment': equipment, 'native_coefficients': coefficients,
            'unresolved': ['missing_bus_assignment',
                           'sources_of_seals_loads_and_admission_fields'],
            'ready_for_final_passport': False}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--snapshot', type=Path, required=True, help='Snapshot XML or its saved structured evidence JSON')
    parser.add_argument('--validation', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    result = build(args.snapshot, args.validation)
    args.out.parent.mkdir(parents=True, exist_ok=True)
    # Keep existing artifacts until the caller explicitly chooses a new output path.
    with args.out.open('x', encoding='utf-8') as handle:
        json.dump(result, handle, ensure_ascii=False, indent=2)
        handle.write('\n')
    print(f'Wrote {len(result["equipment"])} verified equipment records to {args.out}')
