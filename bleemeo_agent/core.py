import copy
import datetime
import io
import json
import logging
import logging.config
import multiprocessing
import os
import signal
import subprocess
import threading
import time

import apscheduler.schedulers.blocking
import psutil
from six.moves import configparser

import bleemeo_agent
import bleemeo_agent.bleemeo
import bleemeo_agent.checker
import bleemeo_agent.collectd
import bleemeo_agent.config
import bleemeo_agent.influxdb
import bleemeo_agent.util
import bleemeo_agent.web


KNOWN_SERVICES = [
    {'process': 'apache2', 'service': 'apache', 'port': 80},
    {'process': 'httpd', 'service': 'apache', 'port': 80},
    {'process': 'nginx', 'service': 'nginx', 'port': 80},
    {'process': 'mysqld', 'service': 'mysql', 'port': 3306},
]


def main():
    logging.basicConfig()

    try:
        core = Core()
        core.run()
    except Exception:
        logging.critical(
            'Unhandled error occured. Agent will terminate',
            exc_info=True)
    finally:
        logging.info('Agent stopped')


class StoredValue:
    """ Persistant store for value used by agent.

        Currently store in a json file
    """
    def __init__(self, filename):
        self.filename = filename
        self._content = {}
        self.reload()
        self._write_lock = threading.RLock()

    def reload(self):
        if os.path.exists(self.filename):
            with open(self.filename) as fd:
                self._content = json.load(fd)

    def save(self):
        with self._write_lock:
            try:
                # Don't simply use open. This file must have limited permission
                open_flags = os.O_WRONLY | os.O_CREAT | os.O_TRUNC
                fileno = os.open(self.filename + '.tmp', open_flags, 0o600)
                with os.fdopen(fileno, 'w') as fd:
                    json.dump(self._content, fd)
                    fd.flush()
                    os.fsync(fd.fileno())
                os.rename(self.filename + '.tmp', self.filename)
            except IOError as exc:
                logging.warning('Failed to store file : %s', exc)

    def get(self, key, default=None):
        return self._content.get(key, default)

    def set(self, key, value):
        with self._write_lock:
            self._content[key] = value
            self.save()


class Core:
    def __init__(self):
        self.reload_config()
        self._config_logger()
        logging.info('Agent starting...')
        self.checks = []
        self.last_facts = {}
        self.thresholds = {}
        self.discovered_services = {}
        self.top_info = None

        self.is_terminating = threading.Event()
        self.bleemeo_connector = None
        self.influx_connector = None
        self.collectd_server = None
        self.scheduler = apscheduler.schedulers.blocking.BlockingScheduler()
        self.last_metrics = {}
        self.last_report = datetime.datetime.fromtimestamp(0)

        self._define_thresholds()
        self._schedule_metric_pull()

    def _config_logger(self):
        logger_config = {
            'version': 1,
            'disable_existing_loggers': False,
        }
        logger_config.update(self.config.get('logging', {}))
        logging.config.dictConfig(logger_config)

    def _define_thresholds(self):
        """ Fill self.thresholds from config.thresholds

            It mostly a "copy", only cpu_* are multiplied by the number of
            cpu cores.
        """
        num_core = multiprocessing.cpu_count()
        self.thresholds = copy.deepcopy(self.config.get('thresholds'))
        for key, value in self.thresholds.items():
            if key.startswith('cpu_'):
                for threshold_name in value:
                    value[threshold_name] *= num_core

    def _schedule_metric_pull(self):
        """ Schedule metric which are pulled
        """
        for (name, config) in self.config.get('metric.pull', {}).items():
            interval = config.get('interval', 10)
            self.scheduler.add_job(
                bleemeo_agent.util.pull_raw_metric,
                args=(self, name),
                trigger='interval',
                seconds=interval,
            )

    def run(self):
        try:
            self.setup_signal()
            self._run_discovery()
            self.start_threads()
            self.schedule_tasks()
            bleemeo_agent.checker.initialize_checks(self)
            try:
                self.scheduler.start()
            finally:
                self.scheduler.shutdown()
        except KeyboardInterrupt:
            pass
        finally:
            self.is_terminating.set()

    def setup_signal(self):
        """ Make kill (SIGKILL/SIGQUIT) send a KeyboardInterrupt
        """
        def handler(signum, frame):
            raise KeyboardInterrupt

        signal.signal(signal.SIGTERM, handler)
        signal.signal(signal.SIGQUIT, handler)

    def schedule_tasks(self):
        self.scheduler.add_job(
            bleemeo_agent.checker.periodic_check,
            args=(self,),
            trigger='interval',
            seconds=3,
        )
        self.scheduler.add_job(
            self._purge_metrics,
            trigger='interval',
            minutes=5,
        )
        self.scheduler.add_job(
            self.send_facts,
            next_run_time=datetime.datetime.now(),
            trigger='interval',
            hours=24,
        )
        self.scheduler.add_job(
            self._gather_metrics,
            trigger='interval',
            seconds=10,
        )
        self.scheduler.add_job(
            self.send_top_info,
            trigger='interval',
            seconds=3,
        )

    def start_threads(self):

        if self.config.get('bleemeo.enabled', True):
            self.bleemeo_connector = (
                bleemeo_agent.bleemeo.BleemeoConnector(self))
            self.bleemeo_connector.start()

        if self.config.get('influxdb.enabled', True):
            self.influx_connector = (
                bleemeo_agent.influxdb.InfluxDBConnector(self))
            self.influx_connector.start()

        self.collectd_server = bleemeo_agent.collectd.Collectd(self)
        self.collectd_server.start()

        bleemeo_agent.web.start_server(self)

    def _gather_metrics(self):
        """ Gather and send some metric missing from collectd

            Currently only uptime is sent.
        """
        uptime_seconds = bleemeo_agent.util.get_uptime()
        now = time.time()

        self.emit_metric({
            'measurement': 'uptime',
            'tag': None,
            'status': None,
            'service': None,
            'time': now,
            'value': uptime_seconds,
        })

    def _purge_metrics(self):
        """ Remove old metrics from self.last_metrics

            Some metric may stay in last_metrics unupdated, for example
            disk usage from an unmounted partition.

            For this reason, from time to time, scan last_metrics and drop
            any value older than 6 minutes.
        """
        now = time.time()
        cutoff = now - 60 * 6

        # XXX: concurrent access with emit_metric.
        self.last_metrics = {
            (measurement, tag): metric
            for ((measurement, tag), metric) in self.last_metrics.items()
            if metric['time'] >= cutoff
        }

    def _run_discovery(self):
        """ Try to discover some service based on known port/process
        """
        # First discover local services. It may find docker process
        # if agent is running on docker host, but docker discovery
        # will override local discovery.
        process_names = set()

        for process in psutil.process_iter():
            process_names.add(process.name())

        for service_info in KNOWN_SERVICES:
            if service_info['process'] in process_names:

                self.discovered_services[service_info['service']] = {
                    'port': service_info['port'],
                    'address': '127.0.0.1',
                    'container': 'host',
                }

        # some service may need additionnal information, like password
        if 'mysql' in self.discovered_services:
            self._discover_mysql()

    def _discover_mysql(self):
        """ Find a MySQL user
        """
        mysql_user = 'root'
        mysql_password = ''

        if self.discovered_services['mysql']['container'] == 'host':
            # grab maintenace password from debian.cnf
            try:
                debian_cnf_raw = subprocess.check_output(
                    [
                        'sudo', '--non-interactive',
                        'cat', '/etc/mysql/debian.cnf'
                    ],
                )
            except subprocess.CalledProcessError:
                debian_cnf_raw = ''

            debian_cnf = configparser.SafeConfigParser()
            debian_cnf.readfp(io.StringIO(debian_cnf_raw.decode('utf-8')))
            try:
                mysql_user = debian_cnf.get('client', 'user')
                mysql_password = debian_cnf.get('client', 'password')
            except (configparser.NoSectionError, configparser.NoOptionError):
                pass

        self.discovered_services['mysql']['user'] = mysql_user
        self.discovered_services['mysql']['password'] = mysql_password

    def send_facts(self):
        """ Send facts to Bleemeo SaaS """
        # Note: even if we do not sent them to Bleemeo SaaS, calling this
        # method is still usefull. Web UI use last_facts.
        self.last_facts = bleemeo_agent.util.get_facts(self)
        if self.bleemeo_connector is not None:
            self.bleemeo_connector.send_facts(self.last_facts)

    def send_top_info(self):
        self.top_info = bleemeo_agent.util.get_top_info()
        if self.bleemeo_connector is not None:
            self.bleemeo_connector.publish_top_info(self.top_info)

    def reload_config(self):
        self.config = bleemeo_agent.config.load_config()
        self.stored_values = StoredValue(
            self.config.get(
                'agent.stored_values_file',
                '/var/lib/bleemeo/store.json'))

        return self.config

    def _store_last_value(self, metric):
        """ Store the metric in self.last_matrics, replacing the previous value
        """
        tag = metric['tag']
        measurement = metric['measurement']
        self.last_metrics[(measurement, tag)] = metric

    def emit_metric(self, metric, no_emit=False):
        """ Sent a metric to all configured output

            When no_emit is True, metric is only stored in last_metric
            and not sent to backend (InfluxDB and Bleemeo).
            Usefull for metric needed to compute derivated one like CPU
            usage from one core.
        """
        metric = metric.copy()

        if not no_emit:
            self.check_threshold(metric)

        self._store_last_value(metric)

        if not no_emit:
            if self.config.get('bleemeo.enabled', True):
                self.bleemeo_connector.emit_metric(metric.copy())
            if self.config.get('influxdb.enabled', True):
                self.influx_connector.emit_metric(metric.copy())

    def update_last_report(self):
        self.last_report = max(datetime.datetime.now(), self.last_report)

    def check_threshold(self, metric):
        """ Check if threshold is defined for given metric. If yes, check
            it and add a "status" tag.
        """
        threshold = self.thresholds.get(metric['measurement'])
        if threshold is None:
            return

        value = metric['value']
        if value is None:
            return

        if (threshold.get('low_critical') is not None
                and value < threshold.get('low_critical')):
            status = 'critical'
        elif (threshold.get('low_warning') is not None
                and value < threshold.get('low_warning')):
            status = 'warning'
        elif (threshold.get('high_critical') is not None
                and value > threshold.get('high_critical')):
            status = 'critical'
        elif (threshold.get('high_warning') is not None
                and value > threshold.get('high_warning')):
            status = 'warning'
        else:
            status = 'ok'

        metric['status'] = status

    def get_last_metric(self, name, tag):
        """ Return the last metric matching name and tag.

            None is returned if the metric is not found
        """
        return self.last_metrics.get((name, tag), None)

    def get_last_metric_value(self, name, tag, default=None):
        """ Return value for given metric.

            Return default if metric is not found.
        """
        metric = self.get_last_metric(name, tag)
        if metric is not None:
            return metric['value']
        else:
            return default

    @property
    def agent_uuid(self):
        """ Return a UUID for this agent.

            Currently, it's the UUID assigned by Bleemeo SaaS during
            registration.
        """
        if self.bleemeo_connector is not None:
            return self.bleemeo_connector.agent_uuid
