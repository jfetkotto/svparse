module top (input clk, output logic q, // want
            output [3:0] bus,          // want
            output ok);
  assign ok = clk;
endmodule
