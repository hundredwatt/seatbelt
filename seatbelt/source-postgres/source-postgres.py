#!/usr/bin/env python

import pypgoutput


cdc_reader = pypgoutput.LogicalReplicationReader(
                publication_name="seatbelt_pub",
                slot_name="seatbelt_test_slot",
                host="0.0.0.0",
                database="seatbelt",
                port=55810,
                user="postgres",
                password="postgres",
            )
for message in cdc_reader:
    print(message.model_dump_json(indent=2))

cdc_reader.stop()
