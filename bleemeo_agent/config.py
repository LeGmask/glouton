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

r"""
Load configuration (in yaml) from a "conf.d" folder.

Path to configuration are hardcoded, in this order:

* /etc/bleemeo/agent.conf
* /etc/bleemeo/agent.conf.d/*.conf
* etc/agent.conf
* etc/agent.conf.d/*.conf

Under Windows, paths are:

* C:\ProgramData\Bleemeo\etc\agent.conf
* C:\ProgramData\Bleemeo\etc\agent.conf.d
* etc\agent.conf
* etc\agent.conf.d\*.conf
"""


import functools
import glob
import os

import yaml


PATHS = [
    '/etc/bleemeo/agent.conf',
    '/etc/bleemeo/agent.conf.d',
    'etc/agent.conf',
    'etc/agent.conf.d'
]


WINDOWS_PATHS = [
    r'C:\ProgramData\Bleemeo\etc\agent.conf',
    r'C:\ProgramData\Bleemeo\etc\agent.conf.d',
    r'etc\agent.conf',
    r'etc\agent.conf.d',
]


class Config(dict):
    """
    Work exacly like a normal dict, but "get" method known about sub-dict

    Also add "set" method that known about sub-dict.
    """

    def get(self, name, default=None, separator='.'):
        """ If name contains separator ("." by default), it will search
            in sub-dict.

            Example, if you config is {'category': {'value': 5}}, then
            get('category.value') will return 5.
        """
        current = self
        for path in name.split(separator):
            if path not in current:
                return default
            current = current[path]
        return current

    def set(self, name, value, separator='.'):
        """ If name contains separator ("." by default), it will search
            in sub-dict.

            Example, set(category.value, 5) write result in
            self['category']['value'] = 5.
            It does create intermediary dict as needed (in your example,
            self['category'] = {} if not already an dict).
        """
        current = self
        splitted_name = name.split(separator)
        (paths, last_name) = (splitted_name[:-1], splitted_name[-1])
        for path in paths:
            if not isinstance(current.get(path), dict):
                current[path] = {}
            current = current[path]
        current[last_name] = value


def merge_dict(destination, source):
    """ Merge two dictionary (recursivly). destination is modified

        List are merged by appending source' list to destination' list
    """
    for (key, value) in source.items():
        if (key in destination
                and isinstance(value, dict)
                and isinstance(destination[key], dict)):
            destination[key] = merge_dict(destination[key], value)
        elif (key in destination
                and isinstance(value, list)
                and isinstance(destination[key], list)):
            destination[key].extend(value)
        else:
            destination[key] = value
    return destination


def load_config(paths=None):
    """ Load configuration from given paths (a list) and return a ConfigParser

        If paths is not provided, use default value (PATH, see doc from module)
    """
    if paths is None and os.name == 'nt':
        paths = WINDOWS_PATHS
    elif paths is None:
        paths = PATHS

    default_config = Config()
    errors = []

    configs = [default_config]
    for filepath in config_files(paths):
        try:
            with open(filepath) as fd:
                config = yaml.safe_load(fd)

                # config could be None if file is empty.
                # config could be non-dict if top-level of file is another YAML
                # type, like a list or just a string.
                if config is not None and isinstance(config, dict):
                    configs.append(config)
                elif config is not None:
                    errors.append(
                        'wrong format for file "%s"' % filepath
                    )

        except Exception as exc:
            errors.append(str(exc).replace('\n', ' '))

    return functools.reduce(merge_dict, configs), errors


def config_files(paths):
    """ Return config files present in given paths.

        For each path, if:

        * it is a directory, return all *.conf files inside the directory
        * it is a file, return the path
        * no config file exists for the path, skip it

        So, if path is ['/etc/bleemeo/agent.conf', '/etc/bleemeo/agent.conf.d']
        you will get /etc/bleemeo/agent.conf (if it exists) and all
        existings *.conf under /etc/bleemeo/agent.conf.d
    """
    files = []
    for path in paths:
        if os.path.isfile(path):
            files.append(path)
        elif os.path.isdir(path):
            files.extend(sorted(glob.glob(os.path.join(path, '*.conf'))))

    return files
