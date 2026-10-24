module top (input clk, input [3:0] d, output logic [3:0] q, output logic [3:0] r);
  always_ff @(posedge clk) begin
    q = d;                       // want
  end
  always @(posedge clk or negedge clk) begin
    if (d[0]) r = d;             // want
    else r <= 4'b0;
  end
endmodule
