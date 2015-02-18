#!/usr/bin/env python
#
# Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
#
# execute code for an individual stage
#
import sys
import traceback
import martian

try:
    # Initialize Martian with command line args.
    args = martian.initialize(sys.argv)

    # Load args and retvals from metadata.
    args.set(martian.metadata.read("args"))

    # Execute split code.
    martian.run("stage_defs = martian.module.split(args)")

    # Write the output as JSON.
    martian.metadata.write("stage_defs", stage_defs)

    # Write end of log and completion marker.
    martian.complete()

except Exception as e:
    # If stage code threw an error, package it up as JSON.
    martian.fail()
