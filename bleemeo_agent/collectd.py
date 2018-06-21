#
#  Copyright 2015-2016 Bleemeo
#
#  bleemeo.com an infrastructure monitoring solution in the Cloud
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
#


import logging
import os
import re
import shlex
import subprocess

import bleemeo_agent.other_types
import bleemeo_agent.util


BASE_COLLECTD_CONFIG = """# Configuration generated by Bleemeo-agent.
# do NOT modify, it will be overwrite on next agent start.
"""

APACHE_COLLECTD_CONFIG = """
LoadPlugin apache
<Plugin apache>
    <Instance "bleemeo-%(instance)s">
        URL "http://%(address)s:%(port)s/server-status?auto"
    </Instance>
</Plugin>
"""

BIND_COLLECTD_CONFIG = """
LoadPlugin bind
<Plugin bind>
    URL "http://%(address)s:8053"
    ParseTime       false
    OpCodes         true
    QTypes          true

    ServerStats     true
    ZoneMaintStats  true
    ResolverStats   false
    MemoryStats     true
</Plugin>
"""

MEMCACHED_COLLECTD_CONFIG = """
LoadPlugin memcached
<Plugin memcached>
    <Instance "bleemeo-%(instance)s">
        Host "%(address)s"
        Port "%(port)s"
    </Instance>
</Plugin>
"""

MYSQL_COLLECTD_CONFIG = """
LoadPlugin mysql
<Plugin mysql>
    <Database "bleemeo-%(instance)s">
        Host "%(address)s"
        User "%(username)s"
        Password "%(password)s"
        ConnectTimeout 2
    </Database>
</Plugin>
"""

NGINX_COLLECTD_CONFIG = """
LoadPlugin nginx
<Plugin nginx>
    URL "http://%(address)s:%(port)s/nginx_status"
</Plugin>
"""

NTPD_COLLECTD_CONFIG = """
LoadPlugin ntpd
<Plugin ntpd>
    Host "%(address)s"
    Port "%(port)s"
</Plugin>
"""

OPENLDAP_COLLECTD_CONFIG = """
LoadPlugin openldap
<Plugin openldap>
    <Instance "bleemeo-%(instance)s">
        URL "ldap://%(address)s/"
    </Instance>
</Plugin>
"""

POSTGRESQL_COMMON_COLLECTD_CONFIG = r"""
LoadPlugin postgresql
<Plugin postgresql>
    <Query "bleemeo-transactions">
        Statement "SELECT sum(xact_commit) xact_commit, \
                sum(xact_rollback) xact_rollback \
                FROM pg_stat_database;"
        <Result>
            Type "pg_xact"
            InstancePrefix "commit"
            ValuesFrom "xact_commit"
        </Result>
        <Result>
            Type "pg_xact"
            InstancePrefix "rollback"
            ValuesFrom "xact_rollback"
        </Result>
    </Query>
</Plugin>
"""

POSTGRESQL_COLLECTD_CONFIG = r"""
<Plugin postgresql>
    <Database "postgres">
        Host "%(address)s"
        Port "%(port)s"
        User "%(username)s"
        Password "%(password)s"
        SSLMode "prefer"
        Query "bleemeo-transactions"
        Instance "bleemeo-%(instance)s"
    </Database>
</Plugin>
"""

REDIS_COLLECTD_CONFIG = """
LoadPlugin redis
<Plugin redis>
    <Node "bleemeo-%(instance)s">
        Host "%(address)s"
        Port "%(port)s"
        Timeout 2000
    </Node>
</Plugin>
"""

VARNISH_COLLECTD_CONFIG = """
LoadPlugin varnish
<Plugin "varnish">
    <Instance "">
    </Instance>
</Plugin>
"""

# https://collectd.org/wiki/index.php/Naming_schema
# carbon output change "/" in ".".
# Example of metic name:
# cpu.percent-idle
# df-var-lib.df_complex-free
# disk-sda.disk_octets.read
COLLECTD_REGEX = re.compile(
    r'(?P<plugin>[^-.]+)(-(?P<plugin_instance>[^.]+))?\.'
    r'(?P<type>[^.-]+)([.-](?P<type_instance>.+))?')


class ComputationFail(Exception):
    """ Exceptions raised when computed metrics failed to be computed and
        should not be retried
    """
    pass


class MissingMetric(Exception):
    """ Exceptions raised when a metric needed for a computed metrics is
        not (yet) present. The computed metrics should be retried later.
    """
    pass


def update_discovery(core):
    try:
        _write_config(core)
    except Exception:  # pylint: disable=broad-except
        logging.warning(
            'Failed to write collectd configuration. '
            'Continuing with current configuration')
        logging.debug('exception is:', exc_info=True)


BIND_INSTANCE = None
NGINX_INSTANCE = None


class Collectd:

    def __init__(self, graphite_client):
        self.core = graphite_client.core
        self.graphite_client = graphite_client
        self.graphite_server = graphite_client.server

        self.computed_metrics_pending = set()
        self.last_timestamp = 0

    def close(self):
        self._check_computed_metrics()

    def emit_metric(self, name, timestamp, value):
        # pylint: disable=too-many-return-statements
        # pylint: disable=too-many-branches
        # pylint: disable=too-many-statements
        """ Rename a metric and pass it to core

            If the metric is used to compute a derrived metric, add it to
            computed_metrics_pending.

            Nothing is emitted if metric is unknown
        """
        self.graphite_server.data_last_seen_at = bleemeo_agent.util.get_clock()

        if timestamp - self.last_timestamp > 1:
            self._check_computed_metrics()
        self.last_timestamp = timestamp

        # the first component is the hostname
        name = name.split('.', 1)[1]
        match = COLLECTD_REGEX.match(name)
        if match is None:
            return
        match_dict = match.groupdict()

        item = ''
        service = None

        if match_dict['plugin'] == 'cpu':
            name = 'cpu_%s' % match_dict['type_instance']
            if name == 'cpu_idle':
                self.core.emit_metric(
                    bleemeo_agent.other_types.MetricPoint(
                        label='cpu_used',
                        time=timestamp,
                        value=100 - value,
                        item='',
                        service_label='',
                        service_instance='',
                        container_name='',
                        status_code=None,
                        status_of='',
                        problem_origin='',
                    )
                )
            self.computed_metrics_pending.add(
                ('cpu_other', '', '', timestamp)
            )
        elif match_dict['type'] == 'df_complex':
            name = 'disk_%s' % match_dict['type_instance']
            path = match_dict['plugin_instance']
            if path == 'root':
                path = '/'
            else:
                path = '/' + path.replace('-', '/')
            path = self.graphite_server.disk_path_rename(path)
            if path is None:
                # this partition is ignored
                return

            item = path
            self.computed_metrics_pending.add(
                ('disk_total', item, '', timestamp)
            )
        elif match_dict['plugin'] == 'disk':
            if match_dict['type_instance'] == 'io_time':
                name = 'io_time'
            elif match_dict['type_instance'] == 'weighted_io_time':
                name = 'io_time_weighted'
            elif match_dict['type'] == 'pending_operations':
                name = 'io_pending_operations'
            else:
                kind_name = {
                    'disk_merged': '_merged',
                    'disk_octets': '_bytes',
                    'disk_ops': 's',  # will become readS and writeS
                    'disk_time': '_time',
                }[match_dict['type']]
                name = 'io_%s%s' % (match_dict['type_instance'], kind_name)

            item = match_dict['plugin_instance']
            if self.graphite_server.ignored_disk(item):
                return
            if name == 'io_time':
                self.core.emit_metric(
                    bleemeo_agent.other_types.MetricPoint(
                        label='io_utilization',
                        time=timestamp,
                        # io_time is a number of ms spent doing IO
                        # (per seconds) utilization is 100% when we spent
                        # 1000ms during one second
                        value=value / 1000. * 100.,
                        item=item,
                        service_label='',
                        service_instance='',
                        container_name='',
                        status_code=None,
                        status_of='',
                        problem_origin='',
                    )
                )
        elif match_dict['plugin'] == 'interface':
            kind_name = {
                'if_errors': 'err',
                'if_octets': 'bytes',
                'if_packets': 'packets',
            }.get(match_dict['type'])

            if kind_name is None:
                return

            if match_dict['type_instance'] == 'rx':
                direction = 'recv'
            else:
                direction = 'sent'

            item = match_dict['plugin_instance']
            if self.graphite_server.network_interface_blacklist(item):
                return

            # Special cases:
            # * if it's some error, we use "in" and "out"
            # * for bytes, we need to convert it to bits
            if kind_name == 'err':
                direction = (
                    direction
                    .replace('recv', 'in')
                    .replace('sent', 'out')
                )
            elif kind_name == 'bytes':
                kind_name = 'bits'
                value = value * 8

            name = 'net_%s_%s' % (kind_name, direction)
        elif match_dict['plugin'] == 'load':
            duration = {
                'longterm': 15,
                'midterm': 5,
                'shortterm': 1,
            }[match_dict['type_instance']]
            name = 'system_load%s' % duration
        elif match_dict['plugin'] == 'memory':
            name = 'mem_%s' % match_dict['type_instance']
            self.computed_metrics_pending.add(
                ('mem_total', '', '', timestamp)
            )
        elif (match_dict['plugin'] == 'processes'
              and match_dict['type'] == 'fork_rate'):
            name = 'process_fork_rate'
        elif (match_dict['plugin'] == 'processes'
              and match_dict['type'] == 'ps_state'):
            name = 'process_status_%s' % match_dict['type_instance']
            self.computed_metrics_pending.add(
                ('process_total', '', '', timestamp))
        elif match_dict['plugin'] == 'swap' and match_dict['type'] == 'swap':
            if not self.core.last_facts.get('swap_present', False):
                return
            name = 'swap_%s' % match_dict['type_instance']
            self.computed_metrics_pending.add(
                ('swap_total', '', '', timestamp)
            )
        elif (match_dict['plugin'] == 'swap'
              and match_dict['type'] == 'swap_io'):
            if not self.core.last_facts.get('swap_present', False):
                return
            name = 'swap_%s' % match_dict['type_instance']
        elif match_dict['plugin'] == 'users':
            name = 'users_logged'
        elif (match_dict['plugin'] == 'apache'
              and match_dict['plugin_instance'].startswith('bleemeo-')):
            name = match_dict['type']
            if match_dict['type_instance']:
                name += '_' + match_dict['type_instance']

            item = match_dict['plugin_instance'].replace('bleemeo-', '')
            if item == 'None':
                item = ''
            service = 'apache'
        elif (match_dict['plugin'] == 'mysql'
              and match_dict['plugin_instance'].startswith('bleemeo-')):
            name = match_dict['type']
            if match_dict['type_instance']:
                name += '_' + match_dict['type_instance']

            if not name.startswith('mysql_'):
                name = 'mysql_' + name

            name = name.replace('-', '_')

            item = match_dict['plugin_instance'].replace('bleemeo-', '')
            if item == 'None':
                item = ''

            service = 'mysql'
        elif (match_dict['plugin'] == 'postgresql'
              and match_dict['plugin_instance'].startswith('bleemeo-')):
            name = 'postgresql_' + match_dict['type_instance']
            item = match_dict['plugin_instance'].replace('bleemeo-', '')
            if item == 'None':
                item = ''
            service = 'postgresql'
        elif (match_dict['plugin'] == 'redis'
              and match_dict['plugin_instance'].startswith('bleemeo-')):
            name = match_dict['type']
            if match_dict['type_instance']:
                name += '_' + match_dict['type_instance']

            name = 'redis_' + name

            item = match_dict['plugin_instance'].replace('bleemeo-', '')
            if item == 'None':
                item = ''

            service = 'redis'
        elif (match_dict['plugin'] == 'memcached'
              and match_dict['plugin_instance'].startswith('bleemeo-')):
            name = match_dict['type']
            if match_dict['type_instance']:
                name += '_' + match_dict['type_instance']

            if not name.startswith('memcached_'):
                name = 'memcached_' + name

            name = name.replace('.', '_')

            item = match_dict['plugin_instance'].replace('bleemeo-', '')
            if item == 'None':
                item = ''

            service = 'memcached'
        elif (match_dict['plugin'] == 'ntpd'
              and match_dict['type'] == 'time_offset'
              and match_dict['type_instance'] == 'loop'):
            name = 'ntp_time_offset'
            service = 'ntp'
            # value is in ms. Convert it to second
            value = value / 1000.
        elif match_dict['plugin'] == 'bind':
            service = 'bind'
            item = BIND_INSTANCE
            if (match_dict['plugin_instance'] == 'global-qtypes'
                    and match_dict['type'] == 'dns_qtype'):
                name = 'bind_query_%s' % match_dict['type_instance']
            if (match_dict['plugin_instance'] == 'global-opcodes'
                    and match_dict['type'] == 'dns_opcode'
                    and match_dict['type_instance'] == 'QUERY'):
                name = 'bind_requests'
            else:
                return
        elif match_dict['plugin'] == 'nginx':
            service = 'nginx'
            name = match_dict['type'].replace('-', '_')
            item = NGINX_INSTANCE
            if match_dict['type_instance']:
                name += '_' + match_dict['type_instance']
            if not name.startswith('nginx_'):
                name = 'nginx_' + name
        elif (match_dict['plugin'] == 'openldap'
              and match_dict['plugin_instance'].startswith('bleemeo-')):
            service = 'openldap'
            item = match_dict['plugin_instance'].replace('bleemeo-', '')
            if item == 'None':
                item = ''
            name = 'openldap_' + match_dict['type']

            if match_dict['type_instance']:
                name = name + '_' + match_dict['type_instance']

            name = name.replace('-', '_')

            if name == 'openldap_total_connections':
                name = 'openldap_connections_rate'
            elif name == 'openldap_current_connections':
                name = 'openldap_connections'
            elif '_derive_statistics_' in name:
                name = name.replace('_derive_statistics_', '_')
            elif '_derive_waiters_' in name:
                name = name.replace('_derive_waiters_', '_waiters_')
            elif '_threads_threads_' in name:
                name = name.replace('_threads_threads_', '_threads_')

        elif match_dict['plugin'] == 'varnish':
            service = 'varnish'
            if match_dict['plugin_instance'].endswith('connections'):
                name = 'connections_' + match_dict['type_instance']
            elif match_dict['plugin_instance'].endswith('backend'):
                name = (
                    'backend_' +
                    match_dict['type'] +
                    '_' +
                    match_dict['type_instance']
                )
                if 'backend_backends' in name:
                    name = name.replace('backend_backends', 'backend')
            elif match_dict['plugin_instance'].endswith('cache'):
                name = 'cache_' + match_dict['type_instance']
            elif match_dict['plugin_instance'].endswith('shm'):
                name = 'shm_' + match_dict['type_instance']
            else:
                return

            name = 'varnish_' + name.replace('-', '_')

            if name == 'varnish_connections_received':
                name = 'varnish_requests'
            elif name == 'varnish_backend_htt_requests':
                name = 'varnish_backend_requests'
        else:
            return
        if service is None:
            service = ''
        if not item:
            item = ''
        metric_point = bleemeo_agent.other_types.MetricPoint(
            label=name,
            time=timestamp,
            value=value,
            item=item,
            service_label=service,
            service_instance='',
            container_name='',
            status_code=None,
            status_of='',
            problem_origin='',
        )
        self.core.emit_metric(metric_point)

    def packet_finish(self):
        """ Called when graphite_client finished processing one TCP packet
        """
        self._check_computed_metrics()

    def _check_computed_metrics(self):
        """ Some metric are computed from other one. For example CPU stats
            are aggregated over all CPUs.

            When any cpu state arrive, we flag the aggregate value as "pending"
            and this function check if stats for all CPU core are fresh enough
            to compute the aggregate.

            This function use computed_metrics_pending, which old a list
            of (metric_name, item, timestamp).
            Item is something like "sda", "sdb" or "eth0", "eth1".
        """
        processed = set()
        for entry in self.computed_metrics_pending:
            (name, item, instance, timestamp) = entry
            try:
                self._compute_metric(name, item, instance, timestamp)
                processed.add(entry)
            except ComputationFail:
                logging.debug(
                    'Failed to compute metric %s at time %s',
                    name, timestamp)
                # we will never be able to recompute it.
                # mark it as done and continue :/
                processed.add(entry)
            except MissingMetric:
                # Some metric are missing to do computing. Wait a bit by
                # keeping this entry in computed_metrics_pending
                pass

        self.computed_metrics_pending.difference_update(processed)

    def _compute_metric(self, name, item, instance, timestamp):
        # pylint: disable=too-many-branches
        def get_metric(measurements, searched_item):
            """ Helper that do common task when retriving metrics:

                * check that metric exists and is not too old
                  (or Raise MissingMetric)
                * If the last metric is more recent that the one we want
                  to compute, raise ComputationFail. We will never be
                  able to compute the requested value.
            """
            metric = self.core.get_last_metric(measurements, searched_item)
            if metric is None or metric['time'] < timestamp:
                raise MissingMetric()
            elif metric['time'] > timestamp:
                raise ComputationFail()
            return metric['value']

        service = None

        if name == 'disk_total':
            used = get_metric('disk_used', item)
            value = used + get_metric('disk_free', item)
            # used_perc could be more that 100% if reserved space is used.
            # We limit it to 100% (105% would be confusing).
            used_perc = min(float(used) / value * 100, 100)

            # But still, total will including reserved space
            value += get_metric('disk_reserved', item)

            self.core.emit_metric(
                bleemeo_agent.other_types.MetricPoint(
                    label=name.replace('_total', '_used_perc'),
                    time=timestamp,
                    value=used_perc,
                    item=item,
                    service_label='',
                    service_instance='',
                    container_name='',
                    status_code=None,
                    status_of='',
                    problem_origin='',
                )
            )
        elif name == 'cpu_other':
            value = get_metric('cpu_used', '')
            value -= get_metric('cpu_user', '')
            value -= get_metric('cpu_system', '')
        elif name == 'mem_total':
            used = get_metric('mem_used', item)
            value = used
            for sub_type in ('buffered', 'cached', 'free'):
                value += get_metric('mem_%s' % sub_type, item)
        elif name == 'process_total':
            types = [
                'blocked', 'paging', 'running', 'sleeping',
                'stopped', 'zombies',
            ]
            value = 0
            for sub_type in types:
                value += get_metric('process_status_%s' % sub_type, item)
        elif name == 'swap_total':
            used = get_metric('swap_used', item)
            value = used + get_metric('swap_free', item)
        else:
            logging.debug('Unknown computed metric %s', name)
            return

        if name in ('mem_total', 'swap_total'):
            if value == 0:
                value_perc = 0.0
            else:
                value_perc = float(used) / value * 100

            self.core.emit_metric(
                bleemeo_agent.other_types.MetricPoint(
                    label=name.replace('_total', '_used_perc'),
                    time=timestamp,
                    value=value_perc,
                    item='',
                    service_label='',
                    service_instance='',
                    container_name='',
                    status_code=None,
                    status_of='',
                    problem_origin='',
                )
            )
        if not item:
            item = ''
        if service is None:
            service = ''
            instance = ''
        metric_point = bleemeo_agent.other_types.MetricPoint(
            label=name,
            time=timestamp,
            value=value,
            item=item,
            service_label=service,
            service_instance=instance,
            container_name='',
            status_code=None,
            status_of='',
            problem_origin='',
        )
        self.core.emit_metric(metric_point)


def _write_config(core):
    collectd_config = _get_collectd_config(core)

    collectd_config_path = core.config.get(
        'collectd.config_file',
        '/etc/collectd/collectd.conf.d/bleemeo-generated.conf'
    )

    if os.path.exists(collectd_config_path):
        with open(collectd_config_path) as config_file:
            current_content = config_file.read()

        if collectd_config == current_content:
            logging.debug('collectd already configured')
            return

    if (collectd_config == BASE_COLLECTD_CONFIG
            and not os.path.exists(collectd_config_path)):
        logging.debug(
            'collectd generated config would be empty, skip writting it'
        )
        return

    # Don't simply use open. This file must have limited permission
    # since it may contains password
    open_flags = os.O_WRONLY | os.O_CREAT | os.O_TRUNC
    fileno = os.open(collectd_config_path, open_flags, 0o600)
    with os.fdopen(fileno, 'w') as config_file:
        config_file.write(collectd_config)

    _restart_collectd(core)


def _get_collectd_config(core):
    # pylint: disable=too-many-branches
    # pylint: disable=global-statement
    global BIND_INSTANCE
    global NGINX_INSTANCE

    has_postgres = False
    collectd_config = BASE_COLLECTD_CONFIG

    sorted_services = sorted(
        core.services.keys(),
        # In couple (service_name, instance) replace instance by an empty
        # string if it's None. Python 3 can not compare None and str.
        key=lambda x: (x[0], x[1] or ""),
    )
    services_type_seen = set()
    for key in sorted_services:
        (service_name, instance) = key

        service_info = core.services[key].copy()
        service_info['instance'] = instance

        if not service_info.get('active', True):
            continue
        if service_info.get('ignore_metrics', False):
            continue

        if service_name == 'apache':
            collectd_config += APACHE_COLLECTD_CONFIG % service_info
        if service_name == 'bind' and 'bind' not in services_type_seen:
            collectd_config += BIND_COLLECTD_CONFIG % service_info
            BIND_INSTANCE = instance
        if service_name == 'memcached':
            collectd_config += MEMCACHED_COLLECTD_CONFIG % service_info
        if (service_name == 'mysql'
                and service_info.get('password') is not None):
            service_info.setdefault('username', 'root')
            collectd_config += MYSQL_COLLECTD_CONFIG % service_info
        if service_name == 'nginx' and 'nginx' not in services_type_seen:
            collectd_config += NGINX_COLLECTD_CONFIG % service_info
            NGINX_INSTANCE = instance
        if service_name == 'ntp':
            collectd_config += NTPD_COLLECTD_CONFIG % service_info
        if service_name == 'openldap':
            collectd_config += OPENLDAP_COLLECTD_CONFIG % service_info
        if (service_name == 'postgresql'
                and service_info.get('password') is not None):
            if not has_postgres:
                collectd_config += POSTGRESQL_COMMON_COLLECTD_CONFIG
                has_postgres = True
            service_info.setdefault('username', 'postgres')
            collectd_config += POSTGRESQL_COLLECTD_CONFIG % service_info
        if service_name == 'redis':
            collectd_config += REDIS_COLLECTD_CONFIG % service_info
        # collectd could only monitor varnish on same host as collectd
        if (service_name == 'varnish'
                and not instance
                and core.config.get('collectd.docker_name') is None):
            collectd_config += VARNISH_COLLECTD_CONFIG % service_info

        services_type_seen.add(service_name)

    return collectd_config


def _restart_collectd(core):
    restart_cmd = core.config.get(
        'collectd.restart_command',
        'sudo -n service collectd restart')
    collectd_container = core.config.get('collectd.docker_name')
    if collectd_container is not None:
        if collectd_container:
            bleemeo_agent.util.docker_restart(
                core.docker_client, collectd_container
            )
    else:
        try:
            output = subprocess.check_output(
                shlex.split(restart_cmd),
                stderr=subprocess.STDOUT,
            )
            return_code = 0
        except (subprocess.CalledProcessError, OSError) as exception:
            output = exception.output
            return_code = exception.returncode

        if return_code != 0:
            logging.info(
                'Failed to restart collectd after reconfiguration: %s',
                output
            )
        else:
            logging.debug(
                'collectd reconfigured and restarted: %s', output)
